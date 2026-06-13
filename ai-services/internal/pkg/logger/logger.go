package logger

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/klog/v2"
)

// Log levels following standard production hierarchy.
const (
	// VerbosityLevelDebug is the klog verbosity level for debug logs (2).
	VerbosityLevelDebug = 2

	// LogLevelDebug is the string constant for debug severity level.
	LogLevelDebug = "debug"
	// LogLevelInfo is the string constant for info severity level.
	LogLevelInfo = "info"
	// LogLevelWarn is the string constant for warning severity level.
	LogLevelWarn = "warning"
	// LogLevelError is the string constant for error severity level.
	LogLevelError = "error"

	// EnvLogLevel is the environment variable name for log severity level (e.g., "info", "debug").
	EnvLogLevel = "AI_SERVICES_LOG_LEVEL"
	// EnvLogFormat is the environment variable name for log format (e.g., "cli", "service").
	EnvLogFormat = "AI_SERVICES_LOG_FORMAT"

	// LogFormatCLI is the string constant for CLI format mode.
	LogFormatCLI = "cli"
	// LogFormatService is the string constant for service format mode.
	LogFormatService = "service"

	// LogLevelInfoIndicator is the output indicator for info level logs ("I").
	LogLevelInfoIndicator = "I"
	// LogLevelWarningIndicator is the output indicator for warning level logs ("W").
	LogLevelWarningIndicator = "W"
	// LogLevelErrorIndicator is the output indicator for error level logs ("E").
	LogLevelErrorIndicator = "E"

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

// Init initializes the logger with appropriate settings based on environment.
func Init() {
	klog.InitFlags(flag.CommandLine)
	_ = flag.CommandLine.Set("alsologtostderr", "true")
	_ = flag.CommandLine.Set("skip_log_backtrace_at", ":0")

	// 1. Resolve Log Format (Defaults to CLI for terminal users)
	logFormat := os.Getenv(EnvLogFormat)
	if logFormat == "" {
		logFormat = LogFormatCLI
	}
	isServiceEnv = logFormat == LogFormatService

	// 2. Resolve Log Severity Level (Defaults to "info")
	logLevel := os.Getenv(EnvLogLevel)
	if logLevel == "" {
		logLevel = LogLevelInfo
	}

	// 3. Apply Format Configuration
	if logFormat == LogFormatCLI {
		_ = flag.CommandLine.Set("skip_headers", "true")
		_ = flag.CommandLine.Set("skip_log_headers", "true")
	} else {
		_ = flag.CommandLine.Set("skip_headers", "true") // Still true because custom wrapper handles the metadata
		_ = flag.CommandLine.Set("logtostderr", "true")
	}

	// 4. Apply Severity Thresholds and set active minimum level
	switch logLevel {
	case LogLevelDebug:
		_ = flag.CommandLine.Set("v", "2")
		activeMinLevel = LevelRankDebug
	case LogLevelWarn:
		_ = flag.CommandLine.Set("v", "0")
		activeMinLevel = LevelRankWarn
	case LogLevelError:
		_ = flag.CommandLine.Set("v", "0")
		activeMinLevel = LevelRankError
	case LogLevelInfo:
		fallthrough
	default:
		_ = flag.CommandLine.Set("v", "0")
		activeMinLevel = LevelRankInfo
	}
}

// getCallerContext generates absolute paths and timestamps if service mode is active.
func getCallerContext(skipDepth int, severity string) string {
	if !isServiceEnv {
		return ""
	}
	_, file, line, ok := runtime.Caller(skipDepth + 1)
	if !ok {
		return ""
	}

	// Use standard klog MMDD format matching production specs
	timestamp := time.Now().UTC().Format("0102 15:04:05.000000")

	// Standardized output shape: "I0610 19:31:44.190447 /path/to/file.go:200] "
	return fmt.Sprintf("%s%s %s:%d] ", severity, timestamp, file, line)
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
	ctx := getCallerContext(1, LogLevelWarningIndicator)
	if ctx == "" {
		klog.WarningDepth(1, "WARNING: ", msg)
	} else {
		klog.WarningDepth(1, ctx, msg)
	}
}

func Warningf(format string, args ...any) {
	if activeMinLevel > LevelRankWarn {
		return
	}
	ctx := getCallerContext(1, LogLevelWarningIndicator)
	formattedMsg := fmt.Sprintf(format, args...)
	if ctx == "" {
		klog.WarningDepth(1, "WARNING: ", formattedMsg)
	} else {
		klog.WarningDepth(1, ctx, formattedMsg)
	}
}

func Errorln(msg string) {
	ctx := getCallerContext(1, LogLevelErrorIndicator)
	if ctx == "" {
		klog.ErrorDepth(1, "ERROR: ", msg)
	} else {
		klog.ErrorDepth(1, ctx, msg)
	}
}

func Errorf(format string, args ...any) {
	ctx := getCallerContext(1, LogLevelErrorIndicator)
	formattedMsg := fmt.Sprintf(format, args...)
	if ctx == "" {
		klog.ErrorDepth(1, "ERROR: ", formattedMsg)
	} else {
		klog.ErrorDepth(1, ctx, formattedMsg)
	}
}

func Infoln(msg string, verbose ...int) {
	// 1. Drop logs early if the environment is explicitly set to warning or error
	if activeMinLevel > LevelRankInfo {
		return
	}

	v := 0
	if len(verbose) > 0 {
		v = verbose[0]
	}

	ctx := getCallerContext(1, LogLevelInfoIndicator)
	klog.V(klog.Level(v)).InfoDepth(1, ctx, msg)
}

func Infof(format string, args ...any) {
	// 1. Drop logs early if the environment is explicitly set to warning or error
	if activeMinLevel > LevelRankInfo {
		return
	}

	v := 0
	// 2. Extract trailing verbosity argument safely to preserve backward compatibility
	if len(args) > 0 {
		if verbosity, ok := args[len(args)-1].(int); ok {
			v = verbosity
			args = args[:len(args)-1]
		}
	}

	ctx := getCallerContext(1, LogLevelInfoIndicator)
	formattedMsg := fmt.Sprintf(format, args...)
	klog.V(klog.Level(v)).InfoDepth(1, ctx, formattedMsg)
}
