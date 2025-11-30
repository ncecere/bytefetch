package worker

import (
	"context"
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"math"
	"math/rand"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/ncecere/bullnose/internal/config"
	"github.com/ncecere/bullnose/internal/db"
	"github.com/ncecere/bullnose/internal/extract"
	"github.com/ncecere/bullnose/internal/fetch"
	formatpkg "github.com/ncecere/bullnose/internal/format"
	"github.com/ncecere/bullnose/internal/logging"
	"github.com/ncecere/bullnose/internal/metrics"
	"github.com/ncecere/bullnose/internal/robots"
	"github.com/ncecere/bullnose/internal/store"
	"github.com/ncecere/bullnose/internal/types"
	"github.com/ncecere/bullnose/internal/urlutil"
)

type taskPlanner interface {
	HasSeenCanonical(ctx context.Context, url string) (bool, error)
	TaskCount(ctx context.Context, jobID string) (int, error)
}

// Start launches the worker loop. Task processing will be implemented next.
func Start(ctx context.Context, cfg *config.Config) error {
	logger := logging.New("worker")
	logger.Info("starting", "pool_size", cfg.Worker.PoolSize, "per_host_limit", cfg.Worker.PerHostLimit, "per_host_rps", cfg.Worker.PerHostRPS)

	sqlDB, err := db.Open(cfg.Database)
	if err != nil {
		return err
	}
	defer sqlDB.Close()

	renderer, err := fetch.NewRenderer(cfg.Browser)
	if err != nil {
		return err
	}
	defer renderer.Close()

	st := store.New(sqlDB)
	fetcher := fetch.NewFetcher(cfg.HTTP, renderer, cfg.Browser)
	limiter := newHostLimiter(cfg.Worker.PerHostLimit, cfg.Worker.PerHostRPS)
	robotsClient := robots.New(cfg.HTTP.Timeout, cfg.Crawl.RobotsFailOpen)

	for i := 0; i < cfg.Worker.PoolSize; i++ {
		go runWorker(ctx, st, fetcher, limiter, robotsClient, cfg, logger.With("worker_id", i))
	}

	<-ctx.Done()
	logger.Info("shutting down")
	time.Sleep(100 * time.Millisecond)
	return nil
}

func runWorker(ctx context.Context, st *store.Store, fetcher *fetch.Fetcher, limiter *hostLimiter, robotsClient *robots.Client, cfg *config.Config, logger *logging.Logger) {
	pollDelay := 500 * time.Millisecond

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		task, err := st.DequeueTask(ctx, cfg.Worker.LeaseTimeout)
		if err != nil {
			if err == sql.ErrNoRows {
				time.Sleep(pollDelay)
				continue
			}
			logger.Error("dequeue error", "err", err)
			time.Sleep(pollDelay)
			continue
		}

		taskCtx, cancel := context.WithCancel(ctx)
		doneCh := make(chan struct{})
		go heartbeatLoop(taskCtx, st, task.ID, cfg.Worker.HeartbeatInterval, doneCh)

		taskStart := time.Now()
		if err := handleTask(taskCtx, st, fetcher, limiter, robotsClient, task, cfg); err != nil {
			logger.Error("task error", "task_id", task.ID, "job_id", task.JobID, "depth", task.Depth, "err", err)
			if shouldRetry(task.Attempts, cfg.Worker.MaxRetries) {
				if retryErr := st.RetryTask(ctx, task.ID, err.Error()); retryErr != nil {
					logger.Error("retry task error", "task_id", task.ID, "err", retryErr)
				}
				time.Sleep(backoff(cfg.Worker.BackoffBase, cfg.Worker.BackoffMax, task.Attempts+1))
			} else {
				if failErr := st.FailTask(ctx, task.ID, err.Error()); failErr != nil {
					logger.Error("fail task", "task_id", task.ID, "err", failErr, "orig", err)
				}
			}
			metrics.WorkerTasksTotal.WithLabelValues("error").Inc()
		} else {
			if completeErr := st.CompleteTask(ctx, task.ID); completeErr != nil {
				logger.Error("complete task error", "task_id", task.ID, "job_id", task.JobID, "err", completeErr)
			}
			metrics.WorkerTasksTotal.WithLabelValues("ok").Inc()
			logger.Info("task done", "task_id", task.ID, "job_id", task.JobID, "depth", task.Depth, "duration_ms", time.Since(taskStart).Milliseconds())
		}
		metrics.WorkerTaskDuration.Observe(time.Since(taskStart).Seconds())

		// Check if job can be marked complete.
		if err := st.FinishJob(ctx, task.JobID); err != nil && err.Error() != "job still has pending tasks" {
			logger.Error("finish job error", "job_id", task.JobID, "err", err)
		}

		close(doneCh)
		cancel()
	}
}

// heartbeatLoop periodically extends the lease while the task is running.
func heartbeatLoop(ctx context.Context, st *store.Store, taskID int64, interval time.Duration, done <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			_ = st.UpdateTaskHeartbeat(ctx, taskID)
		}
	}
}

// handleTask is a placeholder for actual fetch/extract logic.
func handleTask(ctx context.Context, st *store.Store, fetcher *fetch.Fetcher, limiter *hostLimiter, robotsClient *robots.Client, task *store.Task, cfg *config.Config) error {
	job, err := st.GetJob(ctx, task.JobID)
	if err != nil {
		return err
	}
	if cfg.JobDefaults.MaxDuration > 0 && time.Since(job.CreatedAt) > cfg.JobDefaults.MaxDuration {
		_ = st.UpdateJobStatus(ctx, job.ID, store.JobStatusCancelled, "max_duration exceeded")
		return fmt.Errorf("job deadline exceeded")
	}
	if job.Status == store.JobStatusQueued {
		_ = st.StartJob(ctx, job.ID)
	}

	jobCtx := ctx
	if cfg.JobDefaults.MaxDuration > 0 {
		deadline := job.CreatedAt.Add(cfg.JobDefaults.MaxDuration)
		if time.Now().Before(deadline) {
			var cancel context.CancelFunc
			jobCtx, cancel = context.WithDeadline(ctx, deadline)
			defer cancel()
		}
	}

	switch job.Type {
	case "batch_scrape":
		var params map[string]any
		_ = json.Unmarshal(job.Params, &params)
		_, err := processDocumentTask(jobCtx, st, fetcher, limiter, robotsClient, task, params, cfg, job.Type)
		return err
	case "crawl":
		var params map[string]any
		_ = json.Unmarshal(job.Params, &params)
		return processCrawlTask(jobCtx, st, fetcher, limiter, robotsClient, task, params, cfg, job.Type)
	case "map":
		var params map[string]any
		_ = json.Unmarshal(job.Params, &params)
		return processMapTask(jobCtx, st, fetcher, limiter, robotsClient, task, params, cfg, job.Type)
	default:
		return fmt.Errorf("unsupported job type: %s", job.Type)
	}
}

func processDocumentTask(ctx context.Context, st *store.Store, fetcher *fetch.Fetcher, limiter *hostLimiter, robotsClient *robots.Client, task *store.Task, params map[string]any, cfg *config.Config, jobType string) (*extract.Extracted, error) {
	if err := ensureRobotsAllowed(cfg, robotsClient, task.RequestedURL, cfg.HTTP.UserAgent); err != nil {
		return nil, err
	}
	defaultDelay := time.Duration(cfg.Crawl.CrawlDelayMS) * time.Millisecond
	if err := robotsClient.Wait(ctx, task.RequestedURL, cfg.HTTP.UserAgent, defaultDelay); err != nil {
		return nil, err
	}
	return fetchExtractAndStore(ctx, st, limiter, fetcher, task, params, cfg, jobType)
}

func processCrawlTask(ctx context.Context, st *store.Store, fetcher *fetch.Fetcher, limiter *hostLimiter, robotsClient *robots.Client, task *store.Task, params map[string]any, cfg *config.Config, jobType string) error {
	ext, err := processDocumentTask(ctx, st, fetcher, limiter, robotsClient, task, params, cfg, jobType)
	if err != nil {
		return err
	}

	maxDepth := getInt(params, "max_depth")
	sameDomain := getBool(params, "same_domain")
	if task.Depth >= maxDepth {
		return nil
	}
	if cfg.JobDefaults.MaxDuration > 0 && time.Since(task.CreatedAt) > cfg.JobDefaults.MaxDuration {
		return fmt.Errorf("max_duration exceeded")
	}
	includeSitemaps := cfg.Crawl.IncludeSitemaps && getBool(params, "include_sitemaps")
	links := collectLinks(ctx, fetcher, robotsClient, task, ext.Links, sameDomain, includeSitemaps, cfg)
	if len(links) == 0 {
		return nil
	}
	nextDepth := task.Depth + 1
	tasks, err := buildLinkTasks(ctx, st, task.JobID, links, nextDepth, cfg)
	if err != nil {
		return err
	}
	if len(tasks) == 0 {
		return nil
	}
	return st.InsertTasks(ctx, nil, task.JobID, tasks)
}

func processMapTask(ctx context.Context, st *store.Store, fetcher *fetch.Fetcher, limiter *hostLimiter, robotsClient *robots.Client, task *store.Task, params map[string]any, cfg *config.Config, jobType string) error {
	if err := ensureRobotsAllowed(cfg, robotsClient, task.RequestedURL, cfg.HTTP.UserAgent); err != nil {
		return err
	}
	defaultDelay := time.Duration(cfg.Crawl.CrawlDelayMS) * time.Millisecond
	if err := robotsClient.Wait(ctx, task.RequestedURL, cfg.HTTP.UserAgent, defaultDelay); err != nil {
		return err
	}

	res, err := fetchWithLimit(ctx, limiter, fetcher, task.RequestedURL, false)
	if err != nil {
		return err
	}
	ext, err := extract.Extract(res.FinalURL, res.Body)
	if err != nil {
		return err
	}

	maxDepth := getInt(params, "max_depth")
	if task.Depth >= maxDepth {
		return nil
	}
	sameDomain := getBool(params, "same_domain")
	includeSitemaps := cfg.Crawl.IncludeSitemaps && getBool(params, "include_sitemaps")
	links := collectLinks(ctx, fetcher, robotsClient, task, ext.Links, sameDomain, includeSitemaps, cfg)
	if len(links) == 0 {
		return nil
	}
	nextDepth := task.Depth + 1
	tasks, err := buildLinkTasks(ctx, st, task.JobID, links, nextDepth, cfg)
	if err != nil {
		return err
	}
	return enqueueMapLinks(ctx, st, task.JobID, task.RequestedURL, links, nextDepth, tasks)
}

func getBool(params map[string]any, key string) bool {
	if v, ok := params[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

func getString(params map[string]any, key string) string {
	if v, ok := params[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getInt(params map[string]any, key string) int {
	if v, ok := params[key]; ok {
		switch vv := v.(type) {
		case float64:
			return int(vv)
		case int:
			return vv
		}
	}
	return 0
}

func requestedFormats(params map[string]any) []string {
	if raw, ok := params["output_format"]; ok {
		return types.FormatsFromAny(raw, formatpkg.FormatMarkdown)
	}
	return []string{formatpkg.FormatMarkdown}
}

func fetchWithLimit(ctx context.Context, limiter *hostLimiter, fetcher *fetch.Fetcher, rawURL string, useJS bool) (*fetch.Result, error) {
	if limiter != nil {
		release, err := limiter.acquire(ctx, rawURL)
		if err != nil {
			return nil, err
		}
		defer release()
	}
	return fetcher.Fetch(ctx, rawURL, useJS)
}

func ensureRobotsAllowed(cfg *config.Config, robotsClient *robots.Client, url, userAgent string) error {
	if cfg.Crawl.RespectRobots && !robotsClient.Allowed(url, userAgent) {
		return fmt.Errorf("blocked by robots")
	}
	return nil
}

func fetchExtractAndStore(ctx context.Context, st *store.Store, limiter *hostLimiter, fetcher *fetch.Fetcher, task *store.Task, params map[string]any, cfg *config.Config, jobType string) (*extract.Extracted, error) {
	useJS := getBool(params, "use_js")
	formats := requestedFormats(params)

	fetchStart := time.Now()
	res, err := fetchWithLimit(ctx, limiter, fetcher, task.RequestedURL, useJS)
	if err != nil {
		return nil, err
	}
	canonFinal := res.FinalURL
	if cf, err := urlutil.Canonicalize(res.FinalURL, cfg.Crawl.StripQuery); err == nil {
		canonFinal = cf
	}
	host := hostFromURL(canonFinal)
	metrics.FetchDuration.WithLabelValues(host, fmt.Sprintf("%t", useJS)).Observe(time.Since(fetchStart).Seconds())
	if canonFinal != "" && canonFinal != task.FinalURL {
		_ = st.UpdateTaskFinalURL(ctx, task.ID, canonFinal)
	}

	extractStart := time.Now()
	ext, err := extract.Extract(canonFinal, res.Body)
	if err != nil {
		return nil, err
	}
	metrics.ExtractDuration.WithLabelValues(host).Observe(time.Since(extractStart).Seconds())

	if cfg.Crawl.RespectNofollow {
		if robotsVal, ok := ext.Meta["robots"]; ok && strings.Contains(robotsVal, "nofollow") {
			ext.Links = nil
		}
	}

	outputs := make(map[string]string, len(formats))
	var primaryContent string
	for _, fmt := range formats {
		if fmt == "" {
			continue
		}
		if _, ok := outputs[fmt]; ok {
			continue
		}
		content, err := formatpkg.FormatContent(ext, fmt)
		if err != nil {
			return nil, err
		}
		outputs[fmt] = content
		if primaryContent == "" {
			primaryContent = content
		}
	}
	if len(outputs) == 0 {
		content, err := formatpkg.FormatContent(ext, formatpkg.FormatMarkdown)
		if err != nil {
			return nil, err
		}
		outputs[formatpkg.FormatMarkdown] = content
		primaryContent = content
	}

	if err := storeDocument(ctx, st, task, canonFinal, outputs, primaryContent, ext, params, cfg, jobType); err != nil {
		return nil, err
	}
	return ext, nil
}

func storeDocument(ctx context.Context, st *store.Store, task *store.Task, finalURL string, outputs map[string]string, primaryContent string, ext *extract.Extracted, params map[string]any, cfg *config.Config, jobType string) error {
	if ext.Meta == nil {
		ext.Meta = make(map[string]string)
	}
	delete(ext.Meta, "url")
	metaMap := make(map[string]any, len(ext.Meta)+2)
	for k, v := range ext.Meta {
		metaMap[k] = v
	}
	if jobType == "crawl" {
		if start := getString(params, "url"); start != "" {
			metaMap["crawl_url"] = start
		}
	}
	if len(outputs) > 0 {
		metaMap[types.OutputsMetaKey] = outputs
	}
	metaBytes, _ := json.Marshal(metaMap)

	maxLen := len(primaryContent)
	for _, content := range outputs {
		if len(content) > maxLen {
			maxLen = len(content)
		}
	}
	doc := store.Document{
		JobID:        task.JobID,
		RequestedURL: task.RequestedURL,
		FinalURL:     finalURL,
		Title:        ext.Title,
		ContentMD:    primaryContent,
		ContentRaw:   primaryContent,
		Meta:         metaBytes,
	}
	if cfg.JobDefaults.MaxBytes > 0 && int64(maxLen) > cfg.JobDefaults.MaxBytes.Int64() {
		return fmt.Errorf("content exceeds max_bytes")
	}
	if err := st.InsertDocument(ctx, doc); err != nil {
		return err
	}
	if cfg.Crawl.DedupeCache {
		if canon, err := urlutil.Canonicalize(finalURL, cfg.Crawl.StripQuery); err == nil {
			_ = st.MarkCanonical(ctx, canon)
		}
	}
	return nil
}

func collectLinks(ctx context.Context, fetcher *fetch.Fetcher, robotsClient *robots.Client, task *store.Task, rawLinks []string, sameDomain, includeSitemaps bool, cfg *config.Config) []string {
	links := filterLinks(task.RequestedURL, rawLinks, sameDomain, cfg)
	if includeSitemaps && task.Depth == 0 {
		sitemapLinks := discoverSitemapLinks(ctx, fetcher, robotsClient, task, sameDomain, cfg)
		links = appendUniqueLinks(links, sitemapLinks)
	}
	return uniqueStrings(links)
}

func buildLinkTasks(ctx context.Context, st taskPlanner, jobID string, links []string, depth int, cfg *config.Config) ([]store.Task, error) {
	if len(links) == 0 {
		return nil, nil
	}
	unique := uniqueStrings(links)
	tasks := make([]store.Task, 0, len(unique))
	for _, link := range unique {
		if cfg.Crawl.DedupeCache {
			if skipper, ok := any(st).(store.DedupeChecker); ok {
				seen, err := store.ShouldSkipCanonical(ctx, skipper, link)
				if err != nil {
					return nil, err
				}
				if seen {
					continue
				}
			}
		}
		tasks = append(tasks, store.Task{
			RequestedURL: link,
			FinalURL:     link,
			Depth:        depth,
			Status:       store.TaskStatusQueued,
		})
	}
	if len(tasks) == 0 {
		return nil, nil
	}
	if cfg.JobDefaults.MaxTasks > 0 {
		current, err := st.TaskCount(ctx, jobID)
		if err != nil {
			return nil, err
		}
		remaining := cfg.JobDefaults.MaxTasks - current
		if remaining <= 0 {
			return nil, nil
		}
		if len(tasks) > remaining {
			tasks = tasks[:remaining]
		}
	}
	return tasks, nil
}

func enqueueMapLinks(ctx context.Context, st *store.Store, jobID, sourceURL string, links []string, depth int, tasks []store.Task) error {
	if len(links) == 0 {
		return nil
	}
	tx, err := st.DB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	inserts := make([]store.MappedInsert, 0, len(links))
	for _, link := range links {
		inserts = append(inserts, store.MappedInsert{
			JobID:     jobID,
			SourceURL: sourceURL,
			TargetURL: link,
			Depth:     depth,
		})
	}
	if err := st.InsertMappedBatch(ctx, tx, inserts); err != nil {
		tx.Rollback()
		return err
	}
	if len(tasks) > 0 {
		if err := st.InsertTasks(ctx, tx, jobID, tasks); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return values
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, v := range values {
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func shouldRetry(attempts, maxRetries int) bool {
	return attempts < maxRetries
}

func backoff(base, max time.Duration, attempt int) time.Duration {
	if attempt <= 0 {
		attempt = 1
	}
	d := float64(base) * math.Pow(2, float64(attempt-1))
	if d > float64(max) {
		d = float64(max)
	}
	// add jitter +/-20%
	jitter := d * 0.2
	d = d - jitter + (rand.Float64() * 2 * jitter)
	if d > float64(max) {
		d = float64(max)
	}
	return time.Duration(d)
}

func appendUniqueLinks(existing, extra []string) []string {
	if len(extra) == 0 {
		return existing
	}
	seen := make(map[string]struct{}, len(existing))
	for _, link := range existing {
		seen[link] = struct{}{}
	}
	for _, link := range extra {
		if link == "" {
			continue
		}
		if _, ok := seen[link]; ok {
			continue
		}
		existing = append(existing, link)
		seen[link] = struct{}{}
	}
	return existing
}

func filterLinks(baseURL string, links []string, sameDomain bool, cfg *config.Config) []string {
	base, err := url.Parse(baseURL)
	if err != nil {
		return nil
	}
	baseHost := strings.ToLower(stripDefaultPort(base))
	out := make([]string, 0, len(links))
	for _, raw := range links {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		u, err := url.Parse(raw)
		if err != nil {
			continue
		}
		if !u.IsAbs() {
			u = base.ResolveReference(u)
		}
		if !isAllowedScheme(u.Scheme, cfg.Crawl.AllowedSchemes) {
			continue
		}
		u.Host = stripDefaultPort(u)
		targetHost := strings.ToLower(u.Hostname())
		if sameDomain && !sameHostOrSubdomain(baseHost, targetHost) {
			continue
		}
		if cfg.Crawl.StripQuery {
			u.RawQuery = ""
		}
		u.Fragment = ""

		ext := strings.ToLower(path.Ext(u.Path))
		if isBlockedExt(ext, cfg.Crawl.BlockExtensions) {
			continue
		}
		canon, err := urlutil.Canonicalize(u.String(), cfg.Crawl.StripQuery)
		if err != nil {
			continue
		}
		out = append(out, canon)
	}
	return out
}

func discoverSitemapLinks(ctx context.Context, fetcher *fetch.Fetcher, robotsClient *robots.Client, task *store.Task, sameDomain bool, cfg *config.Config) []string {
	if fetcher == nil || robotsClient == nil {
		return nil
	}
	sitemaps := robotsClient.Sitemaps(task.RequestedURL)
	if len(sitemaps) == 0 {
		return nil
	}
	limit := cfg.Crawl.SitemapURLLimit
	if limit <= 0 {
		limit = 1000
	}
	maxDepth := cfg.Crawl.SitemapMaxNested
	if maxDepth <= 0 {
		maxDepth = 1
	}
	raw := make([]string, 0, limit)
	seen := make(map[string]struct{})
	for _, sm := range sitemaps {
		urls, err := fetchSitemapURLs(ctx, fetcher, sm, 1, maxDepth, limit-len(raw), seen)
		if err != nil {
			continue
		}
		raw = append(raw, urls...)
		if len(raw) >= limit {
			break
		}
	}
	if len(raw) == 0 {
		return nil
	}
	filtered := filterLinks(task.RequestedURL, raw, sameDomain, cfg)
	return filtered
}

func fetchSitemapURLs(ctx context.Context, fetcher *fetch.Fetcher, sitemapURL string, depth, maxDepth, remaining int, seen map[string]struct{}) ([]string, error) {
	if remaining <= 0 || depth > maxDepth {
		return nil, nil
	}
	res, err := fetcher.Fetch(ctx, sitemapURL, false)
	if err != nil {
		return nil, err
	}
	body := res.Body
	var set sitemapURLSet
	if err := xml.Unmarshal(body, &set); err == nil && len(set.URLs) > 0 {
		out := make([]string, 0, len(set.URLs))
		for _, u := range set.URLs {
			loc := strings.TrimSpace(u.Loc)
			if loc == "" {
				continue
			}
			if _, ok := seen[loc]; ok {
				continue
			}
			seen[loc] = struct{}{}
			out = append(out, loc)
			if len(out) >= remaining {
				break
			}
		}
		return out, nil
	}

	var idx sitemapIndex
	if err := xml.Unmarshal(body, &idx); err == nil && len(idx.Sitemaps) > 0 {
		collected := make([]string, 0, remaining)
		for _, sm := range idx.Sitemaps {
			loc := strings.TrimSpace(sm.Loc)
			if loc == "" {
				continue
			}
			if _, ok := seen[loc]; ok {
				continue
			}
			seen[loc] = struct{}{}
			urls, err := fetchSitemapURLs(ctx, fetcher, loc, depth+1, maxDepth, remaining-len(collected), seen)
			if err != nil {
				continue
			}
			collected = append(collected, urls...)
			if len(collected) >= remaining {
				break
			}
		}
		return collected, nil
	}
	return nil, nil
}

type sitemapURLSet struct {
	URLs []struct {
		Loc string `xml:"loc"`
	} `xml:"url"`
}

type sitemapIndex struct {
	Sitemaps []struct {
		Loc string `xml:"loc"`
	} `xml:"sitemap"`
}

func isBlockedExt(ext string, blocked []string) bool {
	if ext == "" {
		return false
	}
	for _, b := range blocked {
		if ext == strings.ToLower(b) {
			return true
		}
	}
	return false
}

func isAllowedScheme(scheme string, allowed []string) bool {
	if scheme == "" {
		return false
	}
	scheme = strings.ToLower(scheme)
	for _, a := range allowed {
		if scheme == strings.ToLower(a) {
			return true
		}
	}
	return false
}

func sameHostOrSubdomain(baseHost, targetHost string) bool {
	if baseHost == "" || targetHost == "" {
		return false
	}
	if baseHost == targetHost {
		return true
	}
	return strings.HasSuffix(targetHost, "."+baseHost)
}

func stripDefaultPort(u *url.URL) string {
	if u == nil {
		return ""
	}
	host := u.Host
	switch strings.ToLower(u.Scheme) {
	case "http":
		host = strings.TrimSuffix(host, ":80")
	case "https":
		host = strings.TrimSuffix(host, ":443")
	}
	return host
}
