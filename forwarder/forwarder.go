package forwarder

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"xorkevin.dev/kerrors"
	"xorkevin.dev/klog"
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
		dialer  *net.Dialer
		log     *klog.LevelLogger
		timeout time.Duration
	}
)

func New(log klog.Logger, timeout time.Duration) Forwarder {
	return &server{
		dialer: &net.Dialer{
			KeepAlive: 5 * time.Second,
		},
		log:     klog.NewLevelLogger(log),
		timeout: timeout,
	}
}

func ListenAndForward(ctx context.Context, log klog.Logger, port int, target string, timeout time.Duration) error {
	fwd := New(log, timeout)
	config := net.ListenConfig{
		KeepAlive: 5 * time.Second,
	}
	l, err := config.Listen(ctx, string(TCP), fmt.Sprintf(":%d", port))
	if err != nil {
		return err
	}
	fwd.Forward(ctx, l, target)
	return nil
}

func ListenAndForwardUDP(ctx context.Context, log klog.Logger, port int, target string, timeout time.Duration) error {
	fwd := New(log, timeout)
	config := net.ListenConfig{
		KeepAlive: 5 * time.Second,
	}
	l, err := config.ListenPacket(ctx, string(UDP), fmt.Sprintf(":%d", port))
	if err != nil {
		return err
	}
	fwd.ForwardUDP(ctx, l, target)
	return nil
}

func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

const (
	listenStartDelay = 16 * time.Millisecond
	listenMaxDelay   = 1 * time.Second
)

func (s *server) dialCtx(ctx context.Context, transport Transport, target string) (net.Conn, error) {
	dialCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	return s.dialer.DialContext(dialCtx, string(transport), target)
}

func (s *server) handle(ctx context.Context, wg *sync.WaitGroup, c net.Conn, target string) {
	defer wg.Done()

	var iowg sync.WaitGroup
	defer iowg.Wait()

	// close src and target connections before waiting to prevent deadlock
	// defer src connection close as early as possible to prevent memleak
	defer func() {
		if err := c.Close(); err != nil {
			s.log.Err(ctx, kerrors.WithMsg(err, "Failed to close listener conn"))
		} else {
			s.log.Debug(ctx, "Close listener conn")
		}
	}()

	ctx = klog.CtxWithAttrs(ctx, klog.AString("srcconn", c.RemoteAddr().String()))
	s.log.Debug(ctx, "Accepted conn")

	t, err := s.dialCtx(ctx, TCP, target)
	if err != nil {
		s.log.Err(ctx, kerrors.WithMsg(err, fmt.Sprintf("Failed to connect to target: %s %s", TCP, target)))
		return
	}
	defer func() {
		if err := t.Close(); err != nil {
			s.log.Err(ctx, kerrors.WithMsg(err, "Failed to close target conn"))
		} else {
			s.log.Debug(ctx, "Closed target conn")
		}
	}()
	ctx = klog.CtxWithAttrs(ctx, klog.AString("targetconn", t.RemoteAddr().String()))
	s.log.Debug(ctx, "Opened target conn")

	fwdCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	iowg.Add(1)
	go func() {
		defer iowg.Done()
		defer cancel()
		_, err := io.Copy(t, c)
		if err != nil {
			s.log.Debug(fwdCtx, "Error writing to target conn", klog.AString("err", err.Error()))
		} else {
			s.log.Debug(fwdCtx, "Done writing to target conn")
		}
	}()
	iowg.Add(1)
	go func() {
		defer iowg.Done()
		defer cancel()
		_, err := io.Copy(c, t)
		if err != nil {
			s.log.Debug(fwdCtx, "Error writing to src conn", klog.AString("err", err.Error()))
		} else {
			s.log.Debug(fwdCtx, "Done writing to src conn")
		}
	}()
	<-fwdCtx.Done()
}

func timeAfter(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("Context closed: %w", context.Cause(ctx))
	case <-t.C:
		return nil
	}
}

func (s *server) Forward(ctx context.Context, l net.Listener, target string) {
	var wg sync.WaitGroup
	defer wg.Wait()

	// close listener before waiting to prevent new connections
	// defer listener close as early as possible to prevent memleak
	defer func() {
		if err := l.Close(); err != nil {
			s.log.Err(ctx, kerrors.WithMsg(err, "Failed to close net listener"))
		} else {
			s.log.Debug(ctx, "Closed net listener")
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		delay := listenStartDelay
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			c, err := l.Accept()
			if err != nil {
				s.log.Err(ctx, kerrors.WithMsg(err, "Listener accept error"))
				s.log.Debug(ctx, "Backing off", klog.ADuration("delay", delay))
				if err := timeAfter(ctx, delay); err != nil {
					return
				}
				delay = min(delay*2, listenMaxDelay)
				continue
			}

			wg.Add(1)
			go s.handle(ctx, &wg, c, target)

			delay = listenStartDelay
		}
	}()
	<-ctx.Done()
}

type (
	udpListener struct {
		log      *klog.LevelLogger
		l        net.PacketConn
		timeout  time.Duration
		conns    map[string]*udpConn
		connLock sync.RWMutex
		accepts  chan *udpConn
	}

	udpPacket struct {
		buffer []byte
		n      int
	}

	udpConn struct {
		l           *udpListener
		addr        net.Addr
		lastUpdated time.Time
		mu          sync.RWMutex
		read        chan udpPacket
		done        chan struct{}
	}
)

func (p udpPacket) Copy(b []byte) int {
	return copy(b, p.buffer[:p.n])
}

func newUDPListener(log klog.Logger, l net.PacketConn, timeout time.Duration) *udpListener {
	return &udpListener{
		log:      klog.NewLevelLogger(log),
		l:        l,
		timeout:  timeout,
		conns:    map[string]*udpConn{},
		connLock: sync.RWMutex{},
		accepts:  make(chan *udpConn),
	}
}

var udpBuffers = &sync.Pool{
	New: udpBufpoolNew,
}

const (
	udpReceiveMTU = 8192
)

func udpBufpoolNew() any {
	return make([]byte, udpReceiveMTU)
}

func getUDPBuffer() []byte {
	return udpBuffers.Get().([]byte)
}

func putUDPBuffer(buffer []byte) {
	udpBuffers.Put(buffer)
}

func (l *udpListener) Close() error {
	if err := l.l.Close(); err != nil {
		return kerrors.WithMsg(err, "Failed to close udp packet conn")
	}
	return nil
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
		mu:          sync.RWMutex{},
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

		buffer := getUDPBuffer()
		n, addr, err := l.l.ReadFrom(buffer)
		if err != nil {
			l.log.Err(ctx, kerrors.WithMsg(err, "Packet conn accept error"))
			putUDPBuffer(buffer)
			continue
		}

		c, ok := l.getConn(ctx, addr)
		if !ok {
			putUDPBuffer(buffer)
			continue
		}
		select {
		case <-c.done:
			putUDPBuffer(buffer)
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
	return time.Now().After(c.lastUpdated.Add(c.l.timeout))
}

func (c *udpConn) Read(b []byte) (int, error) {
	select {
	case <-c.done:
		return 0, io.EOF
	case p := <-c.read:
		c.refreshLastUpdated()
		n := p.Copy(b)
		putUDPBuffer(p.buffer)
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

func (s *server) handleUDP(ctx context.Context, wg *sync.WaitGroup, c *udpConn, target string) {
	defer wg.Done()

	var iowg sync.WaitGroup
	defer iowg.Wait()

	// close src and target connections before waiting to prevent deadlock
	// defer src connection close as early as possible to prevent memleak
	defer func() {
		if err := c.Close(); err != nil {
			s.log.Err(ctx, kerrors.WithMsg(err, "Failed to close listener conn"))
		} else {
			s.log.Debug(ctx, "Close listener conn")
		}
	}()

	ctx = klog.CtxWithAttrs(ctx, klog.AString("srcconn", c.addr.String()))
	s.log.Debug(ctx, "Accepted conn")

	t, err := s.dialCtx(ctx, UDP, target)
	if err != nil {
		s.log.Err(ctx, kerrors.WithMsg(err, fmt.Sprintf("Failed to connect to target: %s %s", UDP, target)))
		return
	}
	defer func() {
		if err := t.Close(); err != nil {
			s.log.Err(ctx, kerrors.WithMsg(err, "Failed to close target conn"))
		} else {
			s.log.Debug(ctx, "Closed target conn")
		}
	}()

	ctx = klog.CtxWithAttrs(ctx, klog.AString("targetconn", t.RemoteAddr().String()))
	s.log.Debug(ctx, "Opened target conn")

	fwdCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	iowg.Add(1)
	go func() {
		defer iowg.Done()
		defer cancel()
		_, err := io.Copy(t, c)
		if err != nil {
			s.log.Debug(fwdCtx, "Error writing to target conn", klog.AString("err", err.Error()))
		} else {
			s.log.Debug(fwdCtx, "Done writing to target conn")
		}
	}()
	iowg.Add(1)
	go func() {
		defer iowg.Done()
		defer cancel()
		_, err := io.Copy(c, t)
		if err != nil {
			s.log.Debug(ctx, "Error writing to src conn", klog.AString("err", err.Error()))
		} else {
			s.log.Debug(fwdCtx, "Done writing to src conn")
		}
	}()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-fwdCtx.Done():
			return
		case <-ticker.C:
			if c.IsTimedout() {
				return
			}
		}
	}
}

func (s *server) ForwardUDP(ctx context.Context, nl net.PacketConn, target string) {
	var wg sync.WaitGroup
	defer wg.Wait()

	l := newUDPListener(s.log.Logger, nl, s.timeout)
	defer func() {
		if err := l.Close(); err != nil {
			s.log.Err(ctx, kerrors.WithMsg(err, "Failed to close net packet conn"))
		} else {
			s.log.Debug(ctx, "Closed net packet conn")
		}
	}()

	wg.Add(1)
	go l.HandleReads(ctx, &wg)
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

			wg.Add(1)
			go s.handleUDP(ctx, &wg, c, target)
		}
	}()
	<-ctx.Done()
}
