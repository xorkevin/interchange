package forwarder

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
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
		Forward(ctx context.Context, l net.Listener, target string) error
		ForwardUDP(ctx context.Context, l net.Listener, target string) error
	}

	server struct {
		verbose bool
	}
)

func New(verbose bool) Forwarder {
	return &server{
		verbose: verbose,
	}
}

func ListenAndForward(ctx context.Context, port int, target string, verbose bool) error {
	fwd := New(verbose)
	l, err := net.Listen(string(TCP), fmt.Sprintf(":%v", port))
	if err != nil {
		return err
	}
	return fwd.Forward(ctx, l, target)
}

func ListenAndForwardUDP(ctx context.Context, port int, target string, verbose bool) error {
	fwd := New(verbose)
	l, err := net.Listen(string(UDP), fmt.Sprintf(":%v", port))
	if err != nil {
		return err
	}
	return fwd.ForwardUDP(ctx, l, target)
}

func (s *server) handle(ctx context.Context, wg *sync.WaitGroup, c net.Conn, transport Transport, target string) {
	defer wg.Done()

	done := make(chan struct{})
	dclose := func() {
		close(done)
	}
	donce := &sync.Once{}

	cclose := func() {
		if err := c.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to close listener connection: %v\n", err)
		} else if s.verbose {
			fmt.Fprintf(os.Stderr, "Close listener connection: %v %v\n", c.LocalAddr(), c.RemoteAddr())
		}
	}
	conce := &sync.Once{}
	defer conce.Do(cclose)
	go func() {
		select {
		case <-done:
		case <-ctx.Done():
		}
		conce.Do(cclose)
	}()

	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	d := net.Dialer{}
	t, err := d.DialContext(dialCtx, string(transport), target)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect to target: %v\n", err)
		return
	}
	tclose := func() {
		if err := t.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to close target connection: %v\n", err)
		} else if s.verbose {
			fmt.Fprintf(os.Stderr, "Close target connection: %v %v\n", t.LocalAddr(), t.RemoteAddr())
		}
	}
	tonce := &sync.Once{}
	defer tonce.Do(tclose)
	go func() {
		select {
		case <-done:
		case <-ctx.Done():
		}
		tonce.Do(tclose)
	}()

	if s.verbose {
		fmt.Fprintf(os.Stderr, "Dial target: %v %v\n", t.LocalAddr(), t.RemoteAddr())
	}

	iowg := &sync.WaitGroup{}
	iowg.Add(1)
	go func() {
		defer iowg.Done()
		io.Copy(t, c)
		donce.Do(dclose)
	}()
	iowg.Add(1)
	go func() {
		defer iowg.Done()
		io.Copy(c, t)
		donce.Do(dclose)
	}()
	iowg.Wait()
}

func (s *server) forward(ctx context.Context, l net.Listener, transport Transport, target string) error {
	wg := &sync.WaitGroup{}
	defer wg.Wait()

	lclose := func() {
		if err := l.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to close net listener: %v\n", err)
		} else if s.verbose {
			fmt.Fprintln(os.Stderr, "Close net listener")
		}
	}
	once := &sync.Once{}
	defer once.Do(lclose)
	go func() {
		<-ctx.Done()
		once.Do(lclose)
	}()

	var delay time.Duration
	for {
		c, err := l.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
			}

			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				if delay == 0 {
					delay = 2 * time.Millisecond
				} else {
					delay *= 2
				}
				if max := 1 * time.Second; delay > max {
					delay = max
				}
				fmt.Fprintf(os.Stderr, "TCP Accept error: %v; retrying in %v\n", err, delay)
				time.Sleep(delay)
				continue
			}
			return err
		}
		delay = 0

		if s.verbose {
			fmt.Fprintf(os.Stderr, "Accept connection: %v %v\n", c.LocalAddr(), c.RemoteAddr())
		}
		wg.Add(1)
		go s.handle(ctx, wg, c, transport, target)
	}
}

func (s *server) Forward(ctx context.Context, l net.Listener, target string) error {
	return s.forward(ctx, l, TCP, target)
}

func (s *server) ForwardUDP(ctx context.Context, l net.Listener, target string) error {
	return s.forward(ctx, l, UDP, target)
}
