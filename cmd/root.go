package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"xorkevin.dev/interchange/forwarder"
)

var (
	cfgFile   string
	debugMode bool

	fwdTCPTargets []string
	fwdUDPTargets []string
	fwdVerbose    bool
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
		DisableAutoGenTag: true,
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
		DisableAutoGenTag: true,
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

	fwdCmd.PersistentFlags().StringArrayVarP(&fwdTCPTargets, "tcp", "t", nil, "forward tcp ports (SRCPORT:TARGETHOST:TARGETPORT); may be specified multiple times")
	fwdCmd.PersistentFlags().StringArrayVarP(&fwdUDPTargets, "udp", "u", nil, "forward udp ports (SRCPORT:TARGETHOST:TARGETPORT); may be specified multiple times")
	fwdCmd.PersistentFlags().BoolVarP(&fwdVerbose, "verbose", "v", false, "enable verbose logging")

	// Cobra also supports local flags, which will only run
	// when this action is called directly.
	// rootCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
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

type (
	fwdCmdTarget struct {
		src    int
		target string
	}
)

func runInterchange() error {
	tcpTargets := make([]fwdCmdTarget, 0, len(fwdTCPTargets))
	for _, i := range fwdTCPTargets {
		parts := strings.SplitN(i, ":", 2)
		if len(parts) != 2 {
			return fmt.Errorf("Forward target must be in the form (SRCPORT:TARGETHOST:TARGETPORT)")
		}
		src, err := strconv.Atoi(parts[0])
		if err != nil {
			return fmt.Errorf("Invalid source port: %w", err)
		}
		if src == 0 {
			return fmt.Errorf("Source port cannot be 0")
		}
		target := parts[1]
		if len(target) == 0 {
			return fmt.Errorf("Target not provided")
		}
		tcpTargets = append(tcpTargets, fwdCmdTarget{
			src:    src,
			target: target,
		})
	}
	udpTargets := make([]fwdCmdTarget, 0, len(fwdUDPTargets))
	for _, i := range fwdUDPTargets {
		parts := strings.SplitN(i, ":", 2)
		if len(parts) != 2 {
			return fmt.Errorf("Forward target must be in the form (SRCPORT:TARGETHOST:TARGETPORT)")
		}
		src, err := strconv.Atoi(parts[0])
		if err != nil {
			return fmt.Errorf("Invalid source port: %w", err)
		}
		if src == 0 {
			return fmt.Errorf("Source port cannot be 0")
		}
		target := parts[1]
		if len(target) == 0 {
			return fmt.Errorf("Target not provided")
		}
		udpTargets = append(udpTargets, fwdCmdTarget{
			src:    src,
			target: target,
		})
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup

	for _, i := range tcpTargets {
		k := i
		log.Printf("Forwarding TCP port %d to %s\n", k.src, k.target)
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer cancel()
			if err := forwarder.ListenAndForward(ctx, k.src, k.target, fwdVerbose); err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
		}()
	}
	for _, i := range udpTargets {
		k := i
		log.Printf("Forwarding UDP port %d to %s\n", k.src, k.target)
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer cancel()
			if err := forwarder.ListenAndForwardUDP(ctx, k.src, k.target, fwdVerbose); err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
		}()
	}

	waitForInterrupt(ctx)

	log.Println("Begin shutdown connections")
	cancel()
	wg.Wait()
	return nil
}

func waitForInterrupt(ctx context.Context) {
	notifyCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-notifyCtx.Done()
}
