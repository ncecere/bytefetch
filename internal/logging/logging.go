package logging

import (
	"log/slog"
	"os"
)

// Logger wraps slog for structured logging.
type Logger struct {
	component string
	logger    *slog.Logger
}

func New(component string) *Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{AddSource: false})
	return &Logger{
		component: component,
		logger:    slog.New(handler),
	}
}

func (l *Logger) Info(msg string, kv ...interface{}) {
	l.logger.Info(msg, append([]interface{}{"component", l.component}, kv...)...)
}

func (l *Logger) Error(msg string, kv ...interface{}) {
	l.logger.Error(msg, append([]interface{}{"component", l.component}, kv...)...)
}

func (l *Logger) With(kv ...interface{}) *Logger {
	return &Logger{
		component: l.component,
		logger:    l.logger.With(kv...),
	}
}
