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
	"time"

	"github.com/spf13/cobra"
	"xorkevin.dev/interchange/forwarder"
	"xorkevin.dev/kerrors"
	"xorkevin.dev/klog"
)

type (
	fwdFlags struct {
		tcp        []string
		udp        []string
		tcptimeout time.Duration
		udptimeout time.Duration
	}

	fwdTarget struct {
		src    int
		target string
	}
)

func (c *Cmd) getFwdCmd() *cobra.Command {
	fwdCmd := &cobra.Command{
		Use:               "fwd",
		Short:             "Forwards tcp and udp traffic",
		Long:              `Forwards tcp and udp traffic`,
		Run:               c.execFwdCmd,
		DisableAutoGenTag: true,
	}

	fwdCmd.PersistentFlags().StringArrayVarP(&c.fwdFlags.tcp, "tcp", "t", nil, "forward tcp ports (SRCPORT:TARGETHOST:TARGETPORT); may be specified multiple times")
	fwdCmd.PersistentFlags().StringArrayVarP(&c.fwdFlags.udp, "udp", "u", nil, "forward udp ports (SRCPORT:TARGETHOST:TARGETPORT); may be specified multiple times")
	fwdCmd.PersistentFlags().DurationVar(&c.fwdFlags.tcptimeout, "tcp-timeout", 10*time.Second, "tcp conn timeout")
	fwdCmd.PersistentFlags().DurationVar(&c.fwdFlags.udptimeout, "udp-timeout", 10*time.Second, "udp conn timeout")

	return fwdCmd
}

func (c *Cmd) execFwdCmd(cmd *cobra.Command, args []string) {
	tcpTargets, err := parseFwdTargets(c.fwdFlags.tcp)
	if err != nil {
		c.logFatal(err)
		return
	}
	udpTargets, err := parseFwdTargets(c.fwdFlags.udp)
	if err != nil {
		c.logFatal(err)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup

	for _, i := range tcpTargets {
		log.Printf("Forwarding TCP port %d to %s\n", i.src, i.target)
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer cancel()
			if err := forwarder.ListenAndForward(
				ctx,
				c.log.Logger.Sublogger("fwd", klog.AString("transport", "tcp"), klog.AInt("src", i.src), klog.AString("target", i.target)),
				i.src,
				i.target,
				c.fwdFlags.tcptimeout,
			); err != nil {
				c.log.Err(ctx, kerrors.WithMsg(err, fmt.Sprintf("Listener stopped: tcp %d %s", i.src, i.target)))
			}
		}()
	}
	for _, i := range udpTargets {
		log.Printf("Forwarding UDP port %d to %s\n", i.src, i.target)
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer cancel()
			if err := forwarder.ListenAndForwardUDP(
				ctx,
				c.log.Logger.Sublogger("fwd", klog.AString("transport", "udp"), klog.AInt("src", i.src), klog.AString("target", i.target)),
				i.src,
				i.target,
				c.fwdFlags.udptimeout,
			); err != nil {
				c.log.Err(ctx, kerrors.WithMsg(err, fmt.Sprintf("Listener stopped: udp %d %s", i.src, i.target)))
			}
		}()
	}

	waitForInterrupt(ctx)

	c.log.Info(ctx, "Begin shutdown connections")
	cancel()
	wg.Wait()
}

func parseFwdTargets(targets []string) ([]fwdTarget, error) {
	fwdTargets := make([]fwdTarget, 0, len(targets))
	for _, i := range targets {
		srcstr, target, ok := strings.Cut(i, ":")
		if !ok {
			return nil, kerrors.WithMsg(nil, "Forward target must be in the form (SRCPORT:TARGETHOST:TARGETPORT)")
		}
		src, err := strconv.Atoi(srcstr)
		if err != nil {
			return nil, kerrors.WithMsg(err, fmt.Sprintf("Invalid source port: %s", srcstr))
		}
		if src == 0 {
			return nil, kerrors.WithMsg(nil, "Source port cannot be 0")
		}
		if target == "" {
			return nil, kerrors.WithMsg(nil, fmt.Sprintf("Target not provided for src: %s", srcstr))
		}
		fwdTargets = append(fwdTargets, fwdTarget{
			src:    src,
			target: target,
		})
	}
	return fwdTargets, nil
}

func waitForInterrupt(ctx context.Context) {
	notifyCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-notifyCtx.Done()
}
