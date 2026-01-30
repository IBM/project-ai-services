package logger

import (
	"flag"

	"github.com/spf13/cobra"
	"k8s.io/klog/v2"
)

const (
	VerbosityLevelDebug = 2
)

func Init() {
	klog.InitFlags(flag.CommandLine)
	_ = flag.CommandLine.Set("alsologtostderr", "true")
	_ = flag.CommandLine.Set("skip_headers", "true")
	_ = flag.CommandLine.Set("skip_log_headers", "true")
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
	klog.Warningln("WARNING: ", msg)
}

func Warningf(msg string, args ...interface{}) {
	klog.Warningf("WARNING: "+msg, args...)
}

func Errorln(msg string) {
	klog.Errorln("ERROR: ", msg)
}

func Errorf(msg string, args ...interface{}) {
	klog.Errorf("ERROR: "+msg, args...)
}

func Infoln(msg string, verbose ...int) {
	v := 0
	if len(verbose) > 0 {
		v = verbose[0]
	}
	klog.V(klog.Level(v)).Infoln(msg)
}

func Infof(msg string, args ...interface{}) {
	v := 0
	// The last arg is an int, used for verbosity level
	if len(args) > 0 {
		if verbosity, ok := args[len(args)-1].(int); ok {
			v = verbosity
			args = args[:len(args)-1] // remove verbosity argument
		}
	}
	klog.V(klog.Level(v)).Infof(msg, args...)
}
