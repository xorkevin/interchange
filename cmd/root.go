package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/spf13/pflag"
	"xorkevin.dev/interchange/forwarder"
)

var (
	argPort    int
	argTarget  string
	argUdp     bool
	argVerbose bool
	argHelp    bool
)

func Execute() error {
	pflag.IntVarP(&argPort, "port", "p", 0, "listen port for traffic")
	pflag.StringVarP(&argTarget, "target", "t", "", "destination port for traffic (HOST:PORT)")
	pflag.BoolVarP(&argUdp, "udp", "u", false, "forward a udp port")
	pflag.BoolVarP(&argVerbose, "verbose", "v", false, "enable verbose logging")
	pflag.BoolVarP(&argHelp, "help", "h", false, "print this help")
	pflag.Parse()

	if argHelp {
		pflag.Usage()
		return nil
	}

	if argPort == 0 {
		return fmt.Errorf("Source port not provided")
	}
	if len(argTarget) == 0 {
		return fmt.Errorf("Target port not provided")
	}

	transportString := "TCP"
	if argUdp {
		transportString = "UDP"
	}
	log.Printf("Forwarding %s port %d to %s\n", transportString, argPort, argTarget)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer cancel()
		if argUdp {
			if err := forwarder.ListenAndForwardUDP(ctx, argPort, argTarget, argVerbose); err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
		} else {
			if err := forwarder.ListenAndForward(ctx, argPort, argTarget, argVerbose); err != nil {
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
