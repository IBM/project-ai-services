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

// Log levels following standard production hierarchy
const (
	// Verbosity levels for klog
	VerbosityLevelDebug   = 2
	VerbosityLevelInfo    = 0
	VerbosityLevelWarning = 0
	VerbosityLevelError   = 0

	// Standard Severity Levels
	LogLevelDebug = "debug"
	LogLevelInfo  = "info"
	LogLevelWarn  = "warning"
	LogLevelError = "error"

	// Env Log Variables
	EnvLogLevel  = "AI_SERVICES_LOG_LEVEL"  // e.g., "info", "debug"
	EnvLogFormat = "AI_SERVICES_LOG_FORMAT" // e.g., "cli", "service"

	// Log level indicators for output formatting
	LogLevelInfoIndicator    = "I"
	LogLevelWarningIndicator = "W"
	LogLevelErrorIndicator   = "E"
)

// Global state to track whether we are in a service context
var isServiceEnv bool

// Init initializes the logger with appropriate settings based on environment
func Init() {
	klog.InitFlags(flag.CommandLine)
	_ = flag.CommandLine.Set("alsologtostderr", "true")
	_ = flag.CommandLine.Set("skip_log_backtrace_at", ":0")

	// 1. Resolve Log Format (Defaults to "cli" for terminal users)
	logFormat := os.Getenv(EnvLogFormat)
	if logFormat == "" {
		logFormat = "cli"
	}
	isServiceEnv = logFormat == "service"

	// 2. Resolve Log Severity Level (Defaults to "info")
	logLevel := os.Getenv(EnvLogLevel)
	if logLevel == "" {
		logLevel = LogLevelInfo
	}

	// 3. Apply Format Configuration
	if logFormat == "cli" {
		_ = flag.CommandLine.Set("skip_headers", "true")
		_ = flag.CommandLine.Set("skip_log_headers", "true")
	} else {
		_ = flag.CommandLine.Set("skip_headers", "true") // Still true because custom wrapper handles the metadata
		_ = flag.CommandLine.Set("logtostderr", "true")
	}

	// 4. Apply Severity Thresholds
	switch logLevel {
	case LogLevelDebug:
		_ = flag.CommandLine.Set("v", "2")
	case LogLevelInfo:
		_ = flag.CommandLine.Set("v", "0")
	case LogLevelWarn:
		_ = flag.CommandLine.Set("stderrthreshold", "WARNING")
	case LogLevelError:
		_ = flag.CommandLine.Set("stderrthreshold", "ERROR")
	}
}

// getCallerContext generates absolute paths and timestamps if service mode is active
func getCallerContext(skipDepth int, severity string) string {
	if !isServiceEnv {
		return ""
	}
	_, file, line, ok := runtime.Caller(skipDepth + 1)
	if !ok {
		return ""
	}

	// Use standard klog MMDD format or standardized ISO-8601 timestamps
	timestamp := time.Now().Format("0102 15:04:05.000000")

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
	ctx := getCallerContext(1, LogLevelWarningIndicator)
	klog.WarningDepth(1, ctx, msg)
}

func Warningf(msg string, args ...any) {
	ctx := getCallerContext(1, LogLevelWarningIndicator)
	formattedMsg := fmt.Sprintf(msg, args...)
	klog.WarningDepth(1, ctx+formattedMsg)
}

func Errorln(msg string) {
	ctx := getCallerContext(1, LogLevelErrorIndicator)
	klog.ErrorDepth(1, ctx, msg)
}

func Errorf(msg string, args ...any) {
	ctx := getCallerContext(1, LogLevelErrorIndicator)
	formattedMsg := fmt.Sprintf(msg, args...)
	klog.ErrorDepth(1, ctx+formattedMsg)
}

func Infoln(msg string, verbose ...int) {
	v := 0
	if len(verbose) > 0 {
		v = verbose[0]
	}
	ctx := getCallerContext(1, LogLevelInfoIndicator)
	klog.V(klog.Level(v)).InfoDepth(1, ctx, msg)
}

func Infof(msg string, args ...any) {
	v := 0
	// Extract trailing verbosity argument safely to preserve backward compatibility
	if len(args) > 0 {
		if verbosity, ok := args[len(args)-1].(int); ok {
			v = verbosity
			args = args[:len(args)-1]
		}
	}
	ctx := getCallerContext(1, LogLevelInfoIndicator)
	formattedMsg := fmt.Sprintf(msg, args...)
	klog.V(klog.Level(v)).InfoDepth(1, ctx+formattedMsg)
}
