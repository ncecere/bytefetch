package config

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"

	mapstructure "github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

type Config struct {
	Server      ServerConfig    `mapstructure:"server"`
	Database    DatabaseConfig  `mapstructure:"database"`
	HTTP        HTTPConfig      `mapstructure:"http"`
	Worker      WorkerConfig    `mapstructure:"worker"`
	JobDefaults JobDefaults     `mapstructure:"job_defaults"`
	Crawl       CrawlConfig     `mapstructure:"crawl"`
	Browser     BrowserConfig   `mapstructure:"browser"`
	Auth        AuthConfig      `mapstructure:"auth"`
	RateLimit   RateLimitConfig `mapstructure:"ratelimit"`
	Cleanup     CleanupConfig   `mapstructure:"cleanup"`
}

type ServerConfig struct {
	Addr string `mapstructure:"addr"`
}

type DatabaseConfig struct {
	URL          string        `mapstructure:"url"`
	MaxOpenConns int           `mapstructure:"max_open_conns"`
	MaxIdleConns int           `mapstructure:"max_idle_conns"`
	ConnMaxIdle  time.Duration `mapstructure:"conn_max_idle_time"`
	ConnMaxLife  time.Duration `mapstructure:"conn_max_lifetime"`
}

type HTTPConfig struct {
	Timeout      time.Duration `mapstructure:"timeout"`
	MaxRedirects int           `mapstructure:"max_redirects"`
	MaxBodyBytes ByteSize      `mapstructure:"max_body_bytes"`
	UserAgent    string        `mapstructure:"user_agent"`
}

type WorkerConfig struct {
	PoolSize          int           `mapstructure:"pool_size"`
	PerHostLimit      int           `mapstructure:"per_host_limit"`
	PerHostRPS        int           `mapstructure:"per_host_rps"`
	MaxRetries        int           `mapstructure:"max_retries"`
	BackoffBase       time.Duration `mapstructure:"backoff_base"`
	BackoffMax        time.Duration `mapstructure:"backoff_max"`
	LeaseTimeout      time.Duration `mapstructure:"lease_timeout"`
	HeartbeatInterval time.Duration `mapstructure:"heartbeat_interval"`
}

type JobDefaults struct {
	MaxTasks    int           `mapstructure:"max_tasks"`
	MaxDepth    int           `mapstructure:"max_depth"`
	MaxDuration time.Duration `mapstructure:"max_duration"`
	MaxBytes    ByteSize      `mapstructure:"max_bytes"`
	TTL         time.Duration `mapstructure:"ttl"`
	Deadline    time.Duration `mapstructure:"deadline"`
}

type CrawlConfig struct {
	RespectRobots    bool     `mapstructure:"respect_robots"`
	RobotsFailOpen   bool     `mapstructure:"robots_fail_open"`
	RespectNofollow  bool     `mapstructure:"respect_nofollow"`
	CrawlDelayMS     int      `mapstructure:"crawl_delay_ms"`
	StripQuery       bool     `mapstructure:"strip_query"`
	BlockExtensions  []string `mapstructure:"block_extensions"`
	AllowedSchemes   []string `mapstructure:"allowed_schemes"`
	DedupeCache      bool     `mapstructure:"dedupe_cache"`
	IncludeSitemaps  bool     `mapstructure:"include_sitemaps"`
	SitemapURLLimit  int      `mapstructure:"sitemap_url_limit"`
	SitemapMaxNested int      `mapstructure:"sitemap_max_nested"`
}

type BrowserConfig struct {
	Enabled         bool          `mapstructure:"enabled"`
	Headless        bool          `mapstructure:"headless"`
	PoolSize        int           `mapstructure:"pool_size"`
	PerHostLimit    int           `mapstructure:"per_host_limit"`
	NavTimeout      time.Duration `mapstructure:"nav_timeout"`
	WaitIdleTimeout time.Duration `mapstructure:"wait_idle_timeout"`
	ExecutablePath  string        `mapstructure:"executable_path"`
	UserAgent       string        `mapstructure:"user_agent"`
	Viewport        Viewport      `mapstructure:"viewport"`
	MaxPageLifetime time.Duration `mapstructure:"max_page_lifetime"`
}

type Viewport struct {
	Width  int `mapstructure:"width"`
	Height int `mapstructure:"height"`
}

type AuthConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

type RateLimitConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

type CleanupConfig struct {
	Interval       time.Duration `mapstructure:"interval"`
	Enabled        bool          `mapstructure:"enabled"`
	RunIn          []string      `mapstructure:"run_in"`
	APIInterval    time.Duration `mapstructure:"api_interval"`
	WorkerInterval time.Duration `mapstructure:"worker_interval"`
}

// EnabledFor reports whether cleanup should run for the given mode.
func (c CleanupConfig) EnabledFor(mode string) bool {
	if !c.Enabled {
		return false
	}
	if len(c.RunIn) == 0 {
		return true
	}
	for _, m := range c.RunIn {
		if strings.EqualFold(m, mode) {
			return true
		}
	}
	return false
}

// IntervalFor returns the effective interval for a mode (falling back to global interval).
func (c CleanupConfig) IntervalFor(mode string) time.Duration {
	switch mode {
	case "api":
		if c.APIInterval > 0 {
			return c.APIInterval
		}
	case "worker":
		if c.WorkerInterval > 0 {
			return c.WorkerInterval
		}
	}
	return c.Interval
}

type ByteSize int64

// Int64 returns the byte size as int64.
func (b ByteSize) Int64() int64 {
	return int64(b)
}

func Load(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigType("yaml")
	v.SetEnvPrefix("BYTEFETCH")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	applyDefaults(v)

	if path != "" {
		v.SetConfigFile(path)
	} else {
		v.SetConfigName("config")
		v.AddConfigPath(".")
	}

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("read config file: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg,
		viper.DecodeHook(mapstructure.ComposeDecodeHookFunc(
			stringToByteSizeHook(),
			mapstructure.StringToTimeDurationHookFunc(),
		)),
		func(dc *mapstructure.DecoderConfig) { dc.TagName = "mapstructure" },
	); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}

	if cfg.Database.URL == "" {
		cfg.Database.URL = os.Getenv("DATABASE_URL")
	}
	if cfg.Database.URL == "" {
		return nil, fmt.Errorf("database.url is required (or set DATABASE_URL)")
	}

	return &cfg, nil
}

func applyDefaults(v *viper.Viper) {
	v.SetDefault("server.addr", ":8080")

	v.SetDefault("database.max_open_conns", 10)
	v.SetDefault("database.max_idle_conns", 5)
	v.SetDefault("database.conn_max_idle_time", "5m")
	v.SetDefault("database.conn_max_lifetime", "30m")

	v.SetDefault("http.timeout", "10s")
	v.SetDefault("http.max_redirects", 5)
	v.SetDefault("http.max_body_bytes", "5MB")
	v.SetDefault("http.user_agent", "ByteFetchCrawler/0.1")

	v.SetDefault("worker.pool_size", 8)
	v.SetDefault("worker.per_host_limit", 2)
	v.SetDefault("worker.per_host_rps", 2)
	v.SetDefault("worker.max_retries", 3)
	v.SetDefault("worker.backoff_base", "500ms")
	v.SetDefault("worker.backoff_max", "10s")
	v.SetDefault("worker.lease_timeout", "60s")
	v.SetDefault("worker.heartbeat_interval", "10s")

	v.SetDefault("job_defaults.max_tasks", 5000)
	v.SetDefault("job_defaults.max_depth", 2)
	v.SetDefault("job_defaults.max_duration", "15m")
	v.SetDefault("job_defaults.max_bytes", "100MB")
	v.SetDefault("job_defaults.ttl", "24h")
	v.SetDefault("job_defaults.deadline", "15m")

	v.SetDefault("crawl.respect_robots", true)
	v.SetDefault("crawl.robots_fail_open", true)
	v.SetDefault("crawl.respect_nofollow", false)
	v.SetDefault("crawl.crawl_delay_ms", 0)
	v.SetDefault("crawl.strip_query", true)
	v.SetDefault("crawl.block_extensions", []string{".pdf", ".jpg", ".jpeg", ".png", ".gif", ".webp", ".zip", ".tar", ".gz", ".exe", ".dmg", ".iso"})
	v.SetDefault("crawl.allowed_schemes", []string{"http", "https"})
	v.SetDefault("crawl.dedupe_cache", false)
	v.SetDefault("crawl.include_sitemaps", false)
	v.SetDefault("crawl.sitemap_url_limit", 1000)
	v.SetDefault("crawl.sitemap_max_nested", 3)

	v.SetDefault("browser.enabled", true)
	v.SetDefault("browser.headless", true)
	v.SetDefault("browser.pool_size", 4)
	v.SetDefault("browser.per_host_limit", 2)
	v.SetDefault("browser.nav_timeout", "15s")
	v.SetDefault("browser.wait_idle_timeout", "3s")
	v.SetDefault("browser.executable_path", "")
	v.SetDefault("browser.user_agent", "")
	v.SetDefault("browser.viewport.width", 1280)
	v.SetDefault("browser.viewport.height", 720)
	v.SetDefault("browser.max_page_lifetime", "60s")

	v.SetDefault("auth.enabled", false)
	v.SetDefault("ratelimit.enabled", false)
	v.SetDefault("cleanup.interval", "1h")
	v.SetDefault("cleanup.enabled", true)
	v.SetDefault("cleanup.run_in", []string{})
	v.SetDefault("cleanup.api_interval", "0s")
	v.SetDefault("cleanup.worker_interval", "0s")
}

func stringToByteSizeHook() mapstructure.DecodeHookFunc {
	return func(from reflect.Type, to reflect.Type, data interface{}) (interface{}, error) {
		if to != reflect.TypeOf(ByteSize(0)) {
			return data, nil
		}

		switch v := data.(type) {
		case string:
			return parseByteSize(v)
		case int:
			return ByteSize(v), nil
		case int64:
			return ByteSize(v), nil
		case float64:
			return ByteSize(int64(v)), nil
		default:
			return data, nil
		}
	}
}

func parseByteSize(s string) (ByteSize, error) {
	s = strings.TrimSpace(strings.ToUpper(s))
	if s == "" {
		return 0, fmt.Errorf("empty byte size")
	}

	multiplier := int64(1)
	for _, pair := range []struct {
		suffix string
		value  int64
	}{
		{"TB", 1 << 40},
		{"GB", 1 << 30},
		{"MB", 1 << 20},
		{"KB", 1 << 10},
		{"B", 1},
	} {
		if strings.HasSuffix(s, pair.suffix) {
			s = strings.TrimSuffix(s, pair.suffix)
			multiplier = pair.value
			break
		}
	}

	var num float64
	if _, err := fmt.Sscanf(s, "%f", &num); err != nil {
		return 0, fmt.Errorf("parse byte size: %w", err)
	}

	return ByteSize(int64(num * float64(multiplier))), nil
}
