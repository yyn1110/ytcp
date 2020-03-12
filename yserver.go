package ytcp

import (
	"bufio"
	"net"
	"sync"
	"time"
)

type StopMode uint8

const (
	// StopImmediately mean stop directly, the cached data maybe will not send.
	StopImmediately StopMode = iota
	// StopGracefullyButNotWait stop and flush cached data.
	StopGracefullyButNotWait
	// StopGracefullyAndWait stop and block until cached data sended.
	StopGracefullyAndWait
)

type YServer struct {
	Opts *Options

	stopped chan struct{}
	wg      sync.WaitGroup
	mu      sync.Mutex
	once    sync.Once
	lis     net.Listener
	conns   map[*YConn]bool
	addr net.Addr
}

func NewServer(opts *Options) *YServer {
	if opts.RecvBufSize <= 0 {

		opts.RecvBufSize = DefaultRecvBufSize
	}
	if opts.SendBufListLen <= 0 {

		opts.SendBufListLen = DefaultSendBufListLen
	}
	s := &YServer{
		Opts:    opts,
		stopped: make(chan struct{}),
		conns:   make(map[*YConn]bool),
	}
	return s
}

//对所有连接广播
func (s *YServer) BoradCast(p PACK) error {
	for conn, _ := range s.conns {
		err := conn.Send(p)
		if err != nil {
			return err
		}
	}
	return nil
}
func (s *YServer) ListenAndServe(addr string) error {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	s.Serve(l)

	return nil
}
func (s *YServer)Addr() net.Addr{

	return s.addr
}
// Serve start the tcp server to accept.
func (s *YServer) Serve(l net.Listener) {
	defer s.wg.Done()

	s.addr = l.Addr()

	s.wg.Add(1)

	s.mu.Lock()
	s.lis = l
	s.mu.Unlock()

	var tempDelay time.Duration // how long to sleep on accept failure
	maxDelay := 1 * time.Second

	for {
		conn, err := l.Accept()
		if err != nil {
			if nerr, ok := err.(net.Error); ok && nerr.Temporary() {
				if tempDelay == 0 {
					tempDelay = 5 * time.Millisecond
				} else {
					tempDelay *= 2
				}
				if tempDelay > maxDelay {
					tempDelay = maxDelay
				}

				select {
				case <-time.After(tempDelay):
					continue
				case <-s.stopped:
					return
				}
			}

			if !s.IsStopped() {

				s.Stop(StopImmediately)
			}

			return
		}

		tempDelay = 0
		go s.handleRawConn(conn)
	}
}

// IsStopped check if server is stopped.
func (s *YServer) IsStopped() bool {
	select {
	case <-s.stopped:
		return true
	default:
		return false
	}
}

// Stop stops the tcp server.
// StopImmediately: immediately closes all open connections and listener.
// StopGracefullyButNotWait: stops the server and stop all connections gracefully.
// StopGracefullyAndWait: stops the server and blocks until all connections are stopped gracefully.
func (s *YServer) Stop(mode StopMode) {
	s.once.Do(func() {
		close(s.stopped)

		s.mu.Lock()
		lis := s.lis
		s.lis = nil
		conns := s.conns
		s.conns = nil
		s.mu.Unlock()

		if lis != nil {
			lis.Close()
		}

		m := mode
		if m == StopGracefullyAndWait {
			// don't wait each conn stop.
			m = StopGracefullyButNotWait
		}
		for c := range conns {
			c.Stop(m)
		}

		if mode == StopGracefullyAndWait {
			s.wg.Wait()
		}

	})
}

func (s *YServer) handleRawConn(conn net.Conn) {
	s.mu.Lock()
	if s.conns == nil { // s.conns == nil mean server stopped
		s.mu.Unlock()
		conn.Close()
		return
	}
	s.mu.Unlock()

	tcpConn := NewYConn(s.Opts)
	tcpConn.c = conn
	tcpConn.bw = bufio.NewWriter(conn)
	tcpConn.br = bufio.NewReader(conn)
	err:=s.Opts.Handler.CanAccept(tcpConn)
	if err!=nil{
		tcpConn.Stop(StopImmediately)
		return
	}


	if !s.addConn(tcpConn) {
		tcpConn.Stop(StopImmediately)
		return
	}

	s.wg.Add(1)
	defer func() {
		s.removeConn(tcpConn)
		s.wg.Done()
	}()

	s.Opts.Handler.OnAccept(tcpConn)

	tcpConn.serve()
}

func (s *YServer) addConn(conn *YConn) bool {
	s.mu.Lock()
	if s.conns == nil {
		s.mu.Unlock()
		return false
	}
	s.conns[conn] = true

	s.mu.Unlock()
	return true
}

func (s *YServer) removeConn(conn *YConn) {
	s.mu.Lock()
	if s.conns != nil {
		delete(s.conns, conn)
	}

	s.mu.Unlock()
}

// CurClientCount return current client count.
func (s *YServer) CurClientCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.conns)
}
