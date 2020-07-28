package logger

import (
	"flag"
	"github.com/operator-framework/operator-sdk/pkg/log/zap"
	"github.com/spf13/pflag"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// Log - The general base logger. Will be used by all other packages to retrieve their loggers
var Log = logf.Log.WithName("cmd")

func init() {
	parseCommandLine()
	logf.SetLogger(zap.Logger())
}

func parseCommandLine() {
	// Add the zap logger flag set to the CLI. The flag set must
	// be added before calling pflag.Parse().
	pflag.CommandLine.AddFlagSet(zap.FlagSet())

	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)

	pflag.Parse()
}
