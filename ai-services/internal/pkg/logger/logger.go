package logger

import (
	"context"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
	logsv1 "k8s.io/component-base/logs/api/v1"
	_ "k8s.io/component-base/logs/json/register" // Installs JSON driver into logsv1 engine registry
	"k8s.io/klog/v2"
)

// ContextKey is a custom type for context keys to avoid collisions.
type ContextKey string

// RequestIDKey is the context key for storing request ID.
const RequestIDKey ContextKey = "request_id"

// Log levels following standard production hierarchy.
const (
	// verbosityLevelDebug is the klog verbosity level for debug logs (2).
	verbosityLevelDebug = 2

	// LogLevelDebug is the string constant for debug severity level.
	LogLevelDebug = "DEBUG"
	// LogLevelInfo is the string constant for info severity level.
	LogLevelInfo = "INFO"
	// LogLevelWarn is the string constant for warning severity level.
	LogLevelWarn = "WARNING"
	// LogLevelError is the string constant for error severity level.
	LogLevelError = "ERROR"

	// EnvLogLevel is the environment variable name for log severity level (e.g., "info", "debug").
	EnvLogLevel = "AI_SERVICES_LOG_LEVEL"
	// EnvLogFormat is the environment variable name for log format (e.g., "cli", "service").
	EnvLogFormat = "AI_SERVICES_LOG_FORMAT"

	// LogFormatCLI is the string constant for CLI format mode.
	LogFormatCLI = "cli"
	// LogFormatService is the string constant for service format mode.
	LogFormatService = "service"

	// LevelRankDebug is the numeric rank for debug severity level (0).
	LevelRankDebug = iota
	// LevelRankInfo is the numeric rank for info severity level (1).
	LevelRankInfo
	// LevelRankWarn is the numeric rank for warning severity level (2).
	LevelRankWarn
	// LevelRankError is the numeric rank for error severity level (3).
	LevelRankError
)

// Global state tracking log configurations.
var (
	isServiceEnv   bool
	activeMinLevel int
	logOptions     *logsv1.LoggingConfiguration
)

// Init initializes the logger with appropriate settings based on environment.
func Init() {
	// 1. Resolve Log Format (Defaults to CLI for terminal users)
	logFormat := os.Getenv(EnvLogFormat)
	if logFormat == "" {
		logFormat = LogFormatCLI
	}
	isServiceEnv = logFormat == LogFormatService

	// 2. Resolve Log Severity Level (Defaults to "INFO")
	logLevel := strings.ToUpper(os.Getenv(EnvLogLevel))
	if logLevel == "" {
		logLevel = LogLevelInfo
	}

	// 3. Initialize standard Kubernetes Logging Configuration Struct
	logOptions = logsv1.NewLoggingConfiguration()

	// 4. Programmatically apply environment overrides into the API fields
	if isServiceEnv {
		logOptions.Format = "json" // Canonical identifier for JSON driver mapping
		logOptions.Verbosity = logsv1.VerbosityLevel(0)
	} else {
		logOptions.Format = "text"
		logOptions.Verbosity = logsv1.VerbosityLevel(0)

		// Bypasses the default klog boilerplate format in CLI environments
		// This removes timestamps, PIDs, and source file metadata headers [1]
		fs := flag.NewFlagSet("klog-override", flag.ContinueOnError)
		klog.InitFlags(fs)
		_ = fs.Set("logtostderr", "true")
		_ = fs.Set("skip_headers", "true") // Strip default klog headers [1]
	}

	// 5. Apply Severity Thresholds and set active minimum level
	switch logLevel {
	case LogLevelDebug:
		logOptions.Verbosity = logsv1.VerbosityLevel(verbosityLevelDebug)
		activeMinLevel = LevelRankDebug
	case LogLevelWarn:
		activeMinLevel = LevelRankWarn
	case LogLevelError:
		activeMinLevel = LevelRankError
	case LogLevelInfo:
		fallthrough
	default:
		activeMinLevel = LevelRankInfo
	}

	// 6. Bind runtime engine configurations using the official components pipeline
	// This validation step wires up the underlying pluggable JSON encoders
	if err := logsv1.ValidateAndApply(logOptions, nil); err != nil {
		fmt.Fprintf(os.Stderr, "failed to apply component-base logging configurations: %v\n", err)
	}
}

// buildKV builds key-value pairs for structured logging with level, caller_fullpath, and requestID.
// depth specifies how many stack frames to skip when capturing the caller_fullpath location.
func buildKV(ctx context.Context, level string, depth int) []any {
	var kv []any
	kv = append(kv, "level", level)

	// Capture absolute path and line number cleanly
	// depth+1 accounts for buildKV itself in the call stack
	if _, file, line, ok := runtime.Caller(depth + 1); ok {
		// Add absolute file path with line number
		kv = append(kv, "caller_fullpath", fmt.Sprintf("%s:%d", file, line))
	}

	// Extract requestID from context if available
	if ctx != nil {
		if id, ok := ctx.Value(RequestIDKey).(string); ok && id != "" {
			kv = append(kv, "requestID", id)
		}
	}

	return kv
}

func InitFlags(cmd *cobra.Command) {
	klogFlags := flag.NewFlagSet("klog", flag.ExitOnError)
	klog.InitFlags(klogFlags)
	cmd.PersistentFlags().AddGoFlagSet(klogFlags)
}

func Flush() {
	klog.Flush()
}

// Context-aware logging methods (new API).

func WarninglnCtx(ctx context.Context, msg string) {
	if activeMinLevel > LevelRankWarn {
		return
	}
	if isServiceEnv {
		klog.InfoSDepth(1, msg, buildKV(ctx, LogLevelWarn, 1)...)
	} else {
		klog.WarningDepth(1, "WARNING: ", msg)
	}
}

func WarningfCtx(ctx context.Context, format string, args ...any) {
	if activeMinLevel > LevelRankWarn {
		return
	}
	formattedMsg := sanitizedSprintf(format, args...)
	if isServiceEnv {
		klog.InfoSDepth(1, formattedMsg, buildKV(ctx, LogLevelWarn, 1)...)
	} else {
		klog.WarningDepth(1, "WARNING: ", formattedMsg)
	}
}

func ErrorlnCtx(ctx context.Context, msg string) {
	if isServiceEnv {
		klog.InfoSDepth(1, msg, buildKV(ctx, LogLevelError, 1)...)
	} else {
		klog.ErrorDepth(1, "ERROR: ", msg)
	}
}

func ErrorfCtx(ctx context.Context, format string, args ...any) {
	formattedMsg := sanitizedSprintf(format, args...)
	if isServiceEnv {
		klog.InfoSDepth(1, formattedMsg, buildKV(ctx, LogLevelError, 1)...)
	} else {
		klog.ErrorDepth(1, "ERROR: ", formattedMsg)
	}
}

func InfolnCtx(ctx context.Context, msg string) {
	if activeMinLevel > LevelRankInfo {
		return
	}

	if isServiceEnv {
		klog.InfoSDepth(1, msg, buildKV(ctx, LogLevelInfo, 1)...)
	} else {
		klog.InfoDepth(1, msg)
	}
}

func InfofCtx(ctx context.Context, format string, args ...any) {
	if activeMinLevel > LevelRankInfo {
		return
	}

	formattedMsg := sanitizedSprintf(format, args...)
	if isServiceEnv {
		klog.InfoSDepth(1, formattedMsg, buildKV(ctx, LogLevelInfo, 1)...)
	} else {
		klog.InfoDepth(1, formattedMsg)
	}
}

// DebuglnCtx logs a debug message with context and newline (verbosity level 2).
func DebuglnCtx(ctx context.Context, msg string) {
	if activeMinLevel > LevelRankDebug {
		return
	}

	if isServiceEnv {
		klog.V(klog.Level(verbosityLevelDebug)).InfoSDepth(1, msg, buildKV(ctx, LogLevelDebug, 1)...)
	} else {
		klog.V(klog.Level(verbosityLevelDebug)).InfoDepth(1, msg)
	}
}

// DebugfCtx logs a formatted debug message with context (verbosity level 2).
func DebugfCtx(ctx context.Context, format string, args ...any) {
	if activeMinLevel > LevelRankDebug {
		return
	}

	formattedMsg := sanitizedSprintf(format, args...)
	if isServiceEnv {
		klog.V(klog.Level(verbosityLevelDebug)).InfoSDepth(1, formattedMsg, buildKV(ctx, LogLevelDebug, 1)...)
	} else {
		klog.V(klog.Level(verbosityLevelDebug)).InfoDepth(1, formattedMsg)
	}
}

// Backward-compatible methods (old API) - these use context.Background()

func Warningln(msg string) {
	WarninglnCtx(context.Background(), msg)
}

func Warningf(format string, args ...any) {
	WarningfCtx(context.Background(), format, args...)
}

func Errorln(msg string) {
	ErrorlnCtx(context.Background(), msg)
}

func Errorf(format string, args ...any) {
	ErrorfCtx(context.Background(), format, args...)
}

func Infoln(msg string) {
	InfolnCtx(context.Background(), msg)
}

func Infof(format string, args ...any) {
	InfofCtx(context.Background(), format, args...)
}

// Debugln logs a debug message with newline using context.Background().
func Debugln(msg string) {
	DebuglnCtx(context.Background(), msg)
}

// Debugf logs a formatted debug message using context.Background().
func Debugf(format string, args ...any) {
	DebugfCtx(context.Background(), format, args...)
}
