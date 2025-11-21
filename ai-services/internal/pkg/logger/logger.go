package logger

import (
	"flag"

	"k8s.io/klog/v2"
)

func Init() {
	klogFlags := flag.NewFlagSet("klog", flag.ExitOnError)
	klog.InitFlags(klogFlags)
	_ = klogFlags.Set("alsologtostderr", "true")
	_ = klogFlags.Set("skip_headers", "true")
	_ = klogFlags.Set("skip_log_headers", "true")
	_ = klogFlags.Parse([]string{})
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
