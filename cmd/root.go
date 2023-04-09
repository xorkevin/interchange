package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"xorkevin.dev/klog"
)

type (
	Cmd struct {
		rootCmd   *cobra.Command
		log       *klog.LevelLogger
		version   string
		rootFlags rootFlags
		fwdFlags  fwdFlags
		docFlags  docFlags
	}

	rootFlags struct {
		logLevel string
	}
)

func New() *Cmd {
	return &Cmd{}
}

func (c *Cmd) Execute() {
	buildinfo := ReadVCSBuildInfo()
	c.version = buildinfo.ModVersion
	rootCmd := &cobra.Command{
		Use:               "interchange",
		Short:             "A tcp and udp forwarder",
		Long:              `A tcp and udp forwarder`,
		Version:           c.version,
		PersistentPreRun:  c.initConfig,
		DisableAutoGenTag: true,
	}
	rootCmd.PersistentFlags().StringVar(&c.rootFlags.logLevel, "log-level", "info", "log level")
	c.rootCmd = rootCmd

	rootCmd.AddCommand(c.getFwdCmd())
	rootCmd.AddCommand(c.getDocCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
		return
	}
}

func (c *Cmd) initConfig(cmd *cobra.Command, args []string) {
	c.log = klog.NewLevelLogger(klog.New(
		klog.OptHandler(klog.NewJSONSlogHandler(klog.NewSyncWriter(os.Stderr))),
		klog.OptMinLevelStr(c.rootFlags.logLevel),
	))
}

func (c *Cmd) logFatal(err error) {
	c.log.Err(context.Background(), err)
	os.Exit(1)
}
