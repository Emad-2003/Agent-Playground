package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"

	apperrors "crawler-ai/internal/errors"
)

type Options struct {
	Environment string
	Level       string
	Writer      io.Writer
}

var (
	mu            sync.RWMutex
	globalLogger  = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	globalOptions = Options{Environment: "development", Level: "info", Writer: os.Stderr}
)

func Configure(options Options) (*slog.Logger, error) {
	if options.Writer == nil {
		options.Writer = os.Stderr
	}

	if options.Environment == "" {
		options.Environment = "development"
	}

	if options.Level == "" {
		options.Level = "info"
	}

	level, err := parseLevel(options.Level)
	if err != nil {
		return nil, err
	}

	handlerOptions := &slog.HandlerOptions{
		Level:     level,
		AddSource: options.Environment != "production",
	}

	var handler slog.Handler
	if strings.EqualFold(options.Environment, "production") {
		handler = slog.NewJSONHandler(options.Writer, handlerOptions)
	} else {
		handler = slog.NewTextHandler(options.Writer, handlerOptions)
	}

	logger := slog.New(handler)

	mu.Lock()
	globalLogger = logger
	globalOptions = options
	mu.Unlock()

	slog.SetDefault(logger)
	return logger, nil
}

func Logger() *slog.Logger {
	mu.RLock()
	defer mu.RUnlock()
	return globalLogger
}

func OptionsSnapshot() Options {
	mu.RLock()
	defer mu.RUnlock()
	return globalOptions
}

func Debug(msg string, args ...any) {
	Logger().Debug(msg, args...)
}

func Info(msg string, args ...any) {
	Logger().Info(msg, args...)
}

func Warn(msg string, args ...any) {
	Logger().Warn(msg, args...)
}

func Error(msg string, args ...any) {
	Logger().Error(msg, args...)
}

func parseLevel(raw string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, apperrors.New("logging.parseLevel", apperrors.CodeInvalidConfig, "log level must be debug, info, warn, or error")
	}
}
