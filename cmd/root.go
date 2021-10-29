package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"xorkevin.dev/interchange/forwarder"
)

var (
	cfgFile   string
	debugMode bool

	fwdPort    int
	fwdTarget  string
	fwdUdp     bool
	fwdVerbose bool
)

var (
	// rootCmd represents the base command when called without any subcommands
	rootCmd = &cobra.Command{
		Use:   "interchange",
		Short: "A tcp and udp forwarder",
		Long:  `A tcp and udp forwarder`,
		// Uncomment the following line if your bare application
		// has an action associated with it:
		// Run: func(cmd *cobra.Command, args []string) { },
	}

	fwdCmd = &cobra.Command{
		Use:   "fwd",
		Short: "Forwards tcp and udp traffic",
		Long:  `Forwards tcp and udp traffic`,
		// Uncomment the following line if your bare application
		// has an action associated with it:
		Run: func(cmd *cobra.Command, args []string) {
			if err := runInterchange(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		},
	}
)

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.AddCommand(fwdCmd)

	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $XDG_CONFIG_HOME/.interchange.yaml)")
	rootCmd.PersistentFlags().BoolVar(&debugMode, "debug", false, "turn on debug output")

	fwdCmd.PersistentFlags().IntVarP(&fwdPort, "port", "p", 0, "listen port for traffic")
	fwdCmd.PersistentFlags().StringVarP(&fwdTarget, "target", "t", "", "destination port for traffic (HOST:PORT)")
	fwdCmd.PersistentFlags().BoolVarP(&fwdUdp, "udp", "u", false, "forward a udp port")
	fwdCmd.PersistentFlags().BoolVarP(&fwdVerbose, "verbose", "v", false, "enable verbose logging")

	// Cobra also supports local flags, which will only run
	// when this action is called directly.
	//rootCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		viper.SetConfigName(".interchange")
		viper.AddConfigPath(".")

		// Search config in XDG_CONFIG_HOME directory with name ".interchange" (without extension).
		if cfgdir, err := os.UserConfigDir(); err == nil {
			viper.AddConfigPath(cfgdir)
		}
	}

	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	configErr := viper.ReadInConfig()
	if debugMode {
		if configErr == nil {
			fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
		} else {
			fmt.Fprintln(os.Stderr, "Failed reading config file:", configErr)
		}
	}
}

func runInterchange() error {
	if fwdPort == 0 {
		return fmt.Errorf("Source port not provided")
	}
	if len(fwdTarget) == 0 {
		return fmt.Errorf("Target port not provided")
	}

	transportString := "TCP"
	if fwdUdp {
		transportString = "UDP"
	}
	log.Printf("Forwarding %s port %d to %s\n", transportString, fwdPort, fwdTarget)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer cancel()
		if fwdUdp {
			if err := forwarder.ListenAndForwardUDP(ctx, fwdPort, fwdTarget, fwdVerbose); err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
		} else {
			if err := forwarder.ListenAndForward(ctx, fwdPort, fwdTarget, fwdVerbose); err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
		}
	}()

	waitForInterrupt(ctx)
	log.Println("Begin shutdown connections")
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		log.Printf("Failed to close forwarder\n")
	}
	return nil
}

func waitForInterrupt(ctx context.Context) {
	notifyCtx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()
	<-notifyCtx.Done()
}
