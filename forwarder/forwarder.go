package forwarder

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

type (
	Transport string
)

const (
	TCP Transport = "tcp"
	UDP Transport = "udp"
)

type (
	Forwarder interface {
		Forward(ctx context.Context, l net.Listener, target string)
		ForwardUDP(ctx context.Context, l net.Listener, target string)
	}

	server struct {
		verbose bool
		dialer  *net.Dialer
	}
)

func New(verbose bool) Forwarder {
	return &server{
		verbose: verbose,
		dialer: &net.Dialer{
			KeepAlive: 5 * time.Second,
		},
	}
}

func ListenAndForward(ctx context.Context, port int, target string, verbose bool) error {
	fwd := New(verbose)
	l, err := net.Listen(string(TCP), fmt.Sprintf(":%d", port))
	if err != nil {
		return err
	}
	fwd.Forward(ctx, l, target)
	return nil
}

func ListenAndForwardUDP(ctx context.Context, port int, target string, verbose bool) error {
	fwd := New(verbose)
	l, err := net.Listen(string(UDP), fmt.Sprintf(":%d", port))
	if err != nil {
		return err
	}
	fwd.ForwardUDP(ctx, l, target)
	return nil
}

func (s *server) dialCtx(ctx context.Context, transport Transport, target string) (net.Conn, error) {
	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return s.dialer.DialContext(dialCtx, string(transport), target)
}

func (s *server) handle(ctx context.Context, wg *sync.WaitGroup, c net.Conn, transport Transport, target string) {
	defer wg.Done()

	iowg := &sync.WaitGroup{}
	defer iowg.Wait()

	defer func() {
		if err := c.Close(); err != nil {
			log.Printf("Failed to close listener connection: %s %s: %v\n", c.RemoteAddr(), c.LocalAddr(), err)
		} else if s.verbose {
			log.Printf("Close listener connection: %s %s\n", c.RemoteAddr(), c.LocalAddr())
		}
	}()

	t, err := s.dialCtx(ctx, transport, target)
	if err != nil {
		log.Printf("Failed to connect to target: %s %s: %v\n", transport, target, err)
		return
	}
	defer func() {
		if err := t.Close(); err != nil {
			log.Printf("Failed to close target connection: %s %s: %v\n", t.LocalAddr(), t.RemoteAddr(), err)
		} else if s.verbose {
			log.Printf("Close target connection: %s %s\n", t.LocalAddr(), t.RemoteAddr())
		}
	}()

	if s.verbose {
		log.Printf("Opened target connection: %v %v\n", t.LocalAddr(), t.RemoteAddr())
	}

	copyCtx, cancel := context.WithCancel(ctx)

	iowg.Add(1)
	go func() {
		defer iowg.Done()
		defer cancel()
		if _, err := io.Copy(t, c); err != nil {
			log.Printf("Error writing to target connection: %s %s: %v\n", t.LocalAddr(), t.RemoteAddr(), err)
		} else if s.verbose {
			log.Printf("Done writing to target connection: %s %s\n", t.LocalAddr(), t.RemoteAddr())
		}
	}()
	iowg.Add(1)
	go func() {
		defer iowg.Done()
		defer cancel()
		if _, err := io.Copy(c, t); err != nil {
			log.Printf("Error writing to listener connection: %s %s: %v\n", c.RemoteAddr(), c.LocalAddr(), err)
		} else if s.verbose {
			log.Printf("Done writing to listener connection: %s %s\n", c.RemoteAddr(), c.LocalAddr())
		}
	}()
	<-copyCtx.Done()
}

func (s *server) forward(ctx context.Context, l net.Listener, transport Transport, target string) {
	wg := &sync.WaitGroup{}
	defer wg.Wait()

	defer func() {
		if err := l.Close(); err != nil {
			log.Printf("Failed to close net listener: %v\n", err)
		} else if s.verbose {
			log.Printf("Closed net listener\n")
		}
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)
		var delay time.Duration
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			c, err := l.Accept()
			if err != nil {
				if delay == 0 {
					delay = 2 * time.Millisecond
				} else {
					delay *= 2
				}
				if max := time.Second; delay > max {
					delay = max
				}
				if ne, ok := err.(net.Error); ok && ne.Temporary() {
					log.Printf("Temporary network accept error: %v\n", err)
				} else {
					log.Printf("Network accept error: %v\n", err)
				}
				log.Printf("Backing off for %s\n", delay)
				time.Sleep(delay)
				continue
			}
			delay = 0

			if s.verbose {
				log.Printf("Accepted connection: %v %v\n", c.LocalAddr(), c.RemoteAddr())
			}
			wg.Add(1)
			go s.handle(ctx, wg, c, transport, target)
		}
	}()
	<-done
}

func (s *server) Forward(ctx context.Context, l net.Listener, target string) {
	s.forward(ctx, l, TCP, target)
}

func (s *server) ForwardUDP(ctx context.Context, l net.Listener, target string) {
	s.forward(ctx, l, UDP, target)
}
