package cmd

import (
	"context"
	"fmt"
	"github.com/spf13/pflag"
	"os"
	"os/signal"
	"time"
	"xorkevin.dev/interchange/forwarder"
)

var (
	argPort    int
	argTarget  string
	argUdp     bool
	argVerbose bool
	argHelp    bool
)

func Execute() {
	pflag.IntVarP(&argPort, "port", "p", 0, "listen port for traffic")
	pflag.StringVarP(&argTarget, "target", "t", "", "destination port for traffic (HOST:PORT)")
	pflag.BoolVarP(&argUdp, "udp", "u", false, "forward a udp port")
	pflag.BoolVarP(&argVerbose, "verbose", "v", false, "enable verbose logging")
	pflag.BoolVarP(&argHelp, "help", "h", false, "print this help")
	pflag.Parse()

	if argHelp {
		pflag.Usage()
		return
	}

	if argPort == 0 {
		fmt.Fprintln(os.Stderr, "source port not provided")
		os.Exit(1)
	}

	if len(argTarget) == 0 {
		fmt.Fprintln(os.Stderr, "target port not provided")
		os.Exit(1)
	}

	transportString := "TCP"
	if argUdp {
		transportString = "UDP"
	}
	fmt.Printf("Forwarding %s port %d to %s\n", transportString, argPort, argTarget)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		if argUdp {
			if err := forwarder.ListenAndForwardUDP(ctx, argPort, argTarget, argVerbose); err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
		} else {
			if err := forwarder.ListenAndForward(ctx, argPort, argTarget, argVerbose); err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
		}
		close(done)
	}()

	sigShutdown := make(chan os.Signal)
	signal.Notify(sigShutdown, os.Interrupt)
	<-sigShutdown
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		fmt.Fprintln(os.Stderr, "Failed to close forwarder")
	}
}
