package logger

import (
	"context"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	logsv1 "k8s.io/component-base/logs/api/v1"
	_ "k8s.io/component-base/logs/json/register" // Installs JSON driver into logsv1 engine registry
	"k8s.io/klog/v2"
)

// ContextKey is a custom type for context keys to avoid collisions
type ContextKey string

// RequestIDKey is the context key for storing request ID
const RequestIDKey ContextKey = "request_id"

// Log levels following standard production hierarchy.
const (
	// VerbosityLevelDebug is the klog verbosity level for debug logs (2).
	VerbosityLevelDebug = 2

	// Log level string constants (lowercase for env var, uppercase for JSON output)
	LogLevelDebug = "DEBUG"
	LogLevelInfo  = "INFO"
	LogLevelWarn  = "WARNING"
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

// Global state to track whether we are in a service context.
var isServiceEnv bool

// activeMinLevel tracks the active numeric severity level for filtering.
var activeMinLevel int

// requestIDMap stores request IDs per goroutine ID for concurrent request handling
var requestIDMap sync.Map

// logOptions holds the Kubernetes logging configuration
var logOptions *logsv1.LoggingConfiguration

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
	}

	// 5. Apply Severity Thresholds and set active minimum level
	switch logLevel {
	case LogLevelDebug:
		logOptions.Verbosity = logsv1.VerbosityLevel(2)
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

	// 7. Parse flags to apply all settings
	flag.Parse()
}

// SetRequestID sets the request ID in the logger context for the current goroutine
func SetRequestID(ctx context.Context) {
	gid := getGoroutineID()
	requestIDMap.Store(gid, ctx)
}

// ClearRequestID clears the request ID from the logger context for the current goroutine
func ClearRequestID() {
	gid := getGoroutineID()
	requestIDMap.Delete(gid)
}

// getGoroutineID returns the current goroutine ID
func getGoroutineID() uint64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	// Parse goroutine ID from stack trace: "goroutine 123 [running]:"
	var gid uint64
	fmt.Sscanf(string(buf[:n]), "goroutine %d ", &gid)
	return gid
}

// getRequestContext returns the context for the current goroutine if available
// TODO: To be removed once we migrate to have the context based Logger methods
func getRequestContext() context.Context {
	gid := getGoroutineID()
	if ctx, ok := requestIDMap.Load(gid); ok {
		if requestIDContext, ok := ctx.(context.Context); ok {
			return requestIDContext
		}
	}
	return nil
}

// buildKV builds key-value pairs for structured logging with level, caller, and requestID
// depth specifies how many stack frames to skip when capturing the caller location
func buildKV(level string, depth int) []any {
	var kv []any
	kv = append(kv, "level", level)

	// Capture absolute path and line number cleanly
	// depth+1 accounts for buildKV itself in the call stack
	if _, file, line, ok := runtime.Caller(depth + 1); ok {
		// Add absolute file path with line number
		kv = append(kv, "caller", fmt.Sprintf("%s:%d", file, line))
	}

	// Extract requestID from goroutine-local context if available
	ctx := getRequestContext()
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

func Warningln(msg string) {
	if activeMinLevel > LevelRankWarn {
		return
	}
	if isServiceEnv {
		klog.InfoSDepth(1, msg, buildKV(LogLevelWarn, 1)...)
	} else {
		klog.WarningDepth(1, "WARNING: ", msg)
	}
}

func Warningf(format string, args ...any) {
	if activeMinLevel > LevelRankWarn {
		return
	}
	formattedMsg := fmt.Sprintf(format, args...)
	if isServiceEnv {
		klog.InfoSDepth(1, formattedMsg, buildKV(LogLevelWarn, 1)...)
	} else {
		klog.WarningDepth(1, "WARNING: ", formattedMsg)
	}
}

func Errorln(msg string) {
	if isServiceEnv {
		klog.InfoSDepth(1, msg, buildKV(LogLevelError, 1)...)
	} else {
		klog.ErrorDepth(1, "ERROR: ", msg)
	}
}

func Errorf(format string, args ...any) {
	formattedMsg := fmt.Sprintf(format, args...)
	if isServiceEnv {
		klog.InfoSDepth(1, formattedMsg, buildKV(LogLevelError, 1)...)
	} else {
		klog.ErrorDepth(1, "ERROR: ", formattedMsg)
	}
}

func Infoln(msg string, verbose ...int) {
	if activeMinLevel > LevelRankInfo {
		return
	}

	v := 0
	if len(verbose) > 0 {
		v = verbose[0]
	}

	if isServiceEnv {
		klog.V(klog.Level(v)).InfoSDepth(1, msg, buildKV(LogLevelInfo, 1)...)
	} else {
		klog.V(klog.Level(v)).InfoDepth(1, msg)
	}
}

func Infof(format string, args ...any) {
	if activeMinLevel > LevelRankInfo {
		return
	}

	v := 0
	// Extract trailing verbosity argument safely to preserve backward compatibility
	if len(args) > 0 {
		if verbosity, ok := args[len(args)-1].(int); ok {
			v = verbosity
			args = args[:len(args)-1]
		}
	}

	formattedMsg := fmt.Sprintf(format, args...)
	if isServiceEnv {
		klog.V(klog.Level(v)).InfoSDepth(1, formattedMsg, buildKV(LogLevelInfo, 1)...)
	} else {
		klog.V(klog.Level(v)).InfoDepth(1, formattedMsg)
	}
}
