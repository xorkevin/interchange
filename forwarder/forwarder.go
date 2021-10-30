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
		ForwardUDP(ctx context.Context, l net.PacketConn, target string)
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
	config := net.ListenConfig{}
	l, err := config.Listen(ctx, string(TCP), fmt.Sprintf(":%d", port))
	if err != nil {
		return err
	}
	fwd.Forward(ctx, l, target)
	return nil
}

func ListenAndForwardUDP(ctx context.Context, port int, target string, verbose bool) error {
	fwd := New(verbose)
	config := net.ListenConfig{}
	l, err := config.ListenPacket(ctx, string(UDP), fmt.Sprintf(":%d", port))
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
		_, err := io.Copy(t, c)
		if s.verbose {
			if err != nil {
				log.Printf("Error writing to target connection: %s %s: %v\n", t.LocalAddr(), t.RemoteAddr(), err)
			} else {
				log.Printf("Done writing to target connection: %s %s\n", t.LocalAddr(), t.RemoteAddr())
			}
		}
	}()
	iowg.Add(1)
	go func() {
		defer iowg.Done()
		defer cancel()
		_, err := io.Copy(c, t)
		if s.verbose {
			if err != nil {
				log.Printf("Error writing to listener connection: %s %s: %v\n", c.RemoteAddr(), c.LocalAddr(), err)
			} else {
				log.Printf("Done writing to listener connection: %s %s\n", c.RemoteAddr(), c.LocalAddr())
			}
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

	wg.Add(1)
	go func() {
		defer wg.Done()
		var delay time.Duration
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			c, err := l.Accept()
			if err != nil {
				select {
				case <-ctx.Done():
					return
				default:
				}

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
	<-ctx.Done()
}

func (s *server) Forward(ctx context.Context, l net.Listener, target string) {
	s.forward(ctx, l, TCP, target)
}

type (
	udpListener struct {
		l        net.PacketConn
		conns    map[string]*udpConn
		connLock *sync.RWMutex
		accepts  chan *udpConn
		buffers  *sync.Pool
	}

	udpPacket struct {
		buffer []byte
		n      int
	}

	udpConn struct {
		l           *udpListener
		addr        net.Addr
		lastUpdated time.Time
		mu          *sync.RWMutex
		read        chan udpPacket
		done        chan struct{}
	}
)

func (p udpPacket) Copy(b []byte) int {
	return copy(b, p.buffer[:p.n])
}

func newUDPListener(l net.PacketConn) *udpListener {
	return &udpListener{
		l:        l,
		conns:    map[string]*udpConn{},
		connLock: &sync.RWMutex{},
		accepts:  make(chan *udpConn),
		buffers: &sync.Pool{
			New: bufpoolNew,
		},
	}
}

const (
	receiveMTU = 8192
)

func bufpoolNew() interface{} {
	return make([]byte, receiveMTU)
}

func (l *udpListener) getBuffer() []byte {
	return l.buffers.Get().([]byte)
}

func (l *udpListener) putBuffer(buffer []byte) {
	l.buffers.Put(buffer)
}

func (l *udpListener) Close() error {
	return l.l.Close()
}

func (l *udpListener) getConnLocked(addr net.Addr) (*udpConn, bool) {
	c, ok := l.conns[addr.String()]
	return c, ok
}

func (l *udpListener) getConnR(addr net.Addr) (*udpConn, bool) {
	l.connLock.RLock()
	defer l.connLock.RUnlock()
	return l.getConnLocked(addr)
}

func (l *udpListener) getConnW(ctx context.Context, addr net.Addr) (*udpConn, bool) {
	l.connLock.Lock()
	defer l.connLock.Unlock()
	c, ok := l.getConnLocked(addr)
	if ok {
		return c, true
	}
	c = &udpConn{
		l:           l,
		addr:        addr,
		lastUpdated: time.Now(),
		mu:          &sync.RWMutex{},
		read:        make(chan udpPacket),
		done:        make(chan struct{}),
	}
	l.conns[addr.String()] = c
	select {
	case <-ctx.Done():
		return nil, false
	case l.accepts <- c:
	}
	return c, true
}

func (l *udpListener) getConn(ctx context.Context, addr net.Addr) (*udpConn, bool) {
	c, ok := l.getConnR(addr)
	if ok {
		return c, true
	}
	return l.getConnW(ctx, addr)
}

func (l *udpListener) Accept(ctx context.Context) (*udpConn, bool) {
	select {
	case <-ctx.Done():
		return nil, false
	case c := <-l.accepts:
		return c, true
	}
}

func (l *udpListener) HandleReads(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		buffer := l.getBuffer()
		n, addr, err := l.l.ReadFrom(buffer)
		if err != nil {
			l.putBuffer(buffer)
			select {
			case <-ctx.Done():
				return
			default:
			}

			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				log.Printf("Temporary network accept error: %v\n", err)
			} else {
				log.Printf("Network accept error: %v\n", err)
			}
			continue
		}

		c, ok := l.getConn(ctx, addr)
		if !ok {
			l.putBuffer(buffer)
			return
		}
		select {
		case <-c.done:
			l.putBuffer(buffer)
			continue
		case c.read <- udpPacket{
			buffer: buffer,
			n:      n,
		}:
		}
	}
}

func (c *udpConn) refreshLastUpdated() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastUpdated = time.Now()
}

func (c *udpConn) IsTimedout() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return time.Now().After(c.lastUpdated.Add(5 * time.Second))
}

func (c *udpConn) Read(b []byte) (int, error) {
	select {
	case <-c.done:
		return 0, io.EOF
	case p := <-c.read:
		c.refreshLastUpdated()
		n := p.Copy(b)
		c.l.putBuffer(p.buffer)
		return n, nil
	}
}

func (c *udpConn) Write(b []byte) (int, error) {
	c.refreshLastUpdated()
	return c.l.l.WriteTo(b, c.addr)
}

func (c *udpConn) Close() error {
	close(c.done)
	c.l.connLock.Lock()
	defer c.l.connLock.Unlock()
	delete(c.l.conns, c.addr.String())
	return nil
}

func (c *udpConn) LocalAddr() net.Addr {
	return c.l.l.LocalAddr()
}

func (c *udpConn) RemoteAddr() net.Addr {
	return c.addr
}

func (s *server) handleUDP(ctx context.Context, wg *sync.WaitGroup, c *udpConn, transport Transport, target string) {
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
		_, err := io.Copy(t, c)
		if s.verbose {
			if err != nil {
				log.Printf("Error writing to target connection: %s %s: %v\n", t.LocalAddr(), t.RemoteAddr(), err)
			} else {
				log.Printf("Done writing to target connection: %s %s\n", t.LocalAddr(), t.RemoteAddr())
			}
		}
	}()
	iowg.Add(1)
	go func() {
		defer iowg.Done()
		defer cancel()
		_, err := io.Copy(c, t)
		if s.verbose {
			if err != nil {
				log.Printf("Error writing to listener connection: %s %s: %v\n", c.RemoteAddr(), c.LocalAddr(), err)
			} else {
				log.Printf("Done writing to listener connection: %s %s\n", c.RemoteAddr(), c.LocalAddr())
			}
		}
	}()
	iowg.Add(1)
	go func() {
		defer iowg.Done()
		defer cancel()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-copyCtx.Done():
				return
			case <-ticker.C:
				if c.IsTimedout() {
					return
				}
			}
		}
	}()
	<-copyCtx.Done()
}

func (s *server) forwardUDP(ctx context.Context, nl net.PacketConn, transport Transport, target string) {
	wg := &sync.WaitGroup{}
	defer wg.Wait()

	l := newUDPListener(nl)
	defer func() {
		if err := l.Close(); err != nil {
			log.Printf("Failed to close net packet conn: %v\n", err)
		} else if s.verbose {
			log.Printf("Closed net packet conn\n")
		}
	}()

	wg.Add(1)
	go l.HandleReads(ctx, wg)
	wg.Add(1)
	go func() {
		defer wg.Done()

		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			c, ok := l.Accept(ctx)
			if !ok {
				continue
			}

			if s.verbose {
				log.Printf("Accepted connection: %v %v\n", l.l.LocalAddr(), c.addr.String())
			}
			wg.Add(1)
			go s.handleUDP(ctx, wg, c, transport, target)
		}
	}()
	<-ctx.Done()
}

func (s *server) ForwardUDP(ctx context.Context, l net.PacketConn, target string) {
	s.forwardUDP(ctx, l, UDP, target)
}
