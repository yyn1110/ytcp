package ytcp

import (
	"bufio"
	"net"
	"sync"
	"sync/atomic"
	"time"
	"errors"
)

const (
	connStateNormal int32 = iota
	connStateStopping
	connStateStopped
)

type YConn struct {
	sync.Mutex
	Opts     *Options
	c        net.Conn
	id       string
	br       *bufio.Reader
	bw       *bufio.Writer
	closed   chan struct{}
	once     sync.Once
	state    int32
	wg       sync.WaitGroup
	sendChan chan PACK
}

// NewConn return new conn.
func NewYConn(opts *Options) *YConn {
	if opts.RecvBufSize <= 0 {
		opts.RecvBufSize = DefaultRecvBufSize
	}
	if opts.SendBufListLen <= 0 {
		opts.SendBufListLen = DefaultSendBufListLen
	}
	c := &YConn{
		Opts:     opts,
		sendChan: make(chan PACK, opts.SendBufListLen),
		closed:   make(chan struct{}),
		state:    connStateNormal,
	}

	return c
}
func (this *YConn) SendBytes(b []byte) error {
	_,err:=this.bw.Write(b)
	if err != nil {
		return err
	}
	err = this.bw.Flush()
	if err != nil {
		return err
	}
	return nil
}
//Send
func (this *YConn) Send(p PACK) error {
	if atomic.LoadInt32(&this.state) != connStateNormal {
		return errors.New("connect lost")
	}
	this.sendChan<-p
	//_, err := p.Write(this.bw)
	//if err != nil {
	//	return err
	//}
	//err = this.bw.Flush()
	//if err != nil {
	//	return err
	//}
	return nil
}
func (this *YConn) RemoteAddr() string {
	return this.c.RemoteAddr().String()
}
func (this *YConn) String() string {
	return this.c.LocalAddr().String() + " -> " + this.c.RemoteAddr().String()
}
func (this *YConn) Connect(address string) error {
	rawConn, err := net.Dial("tcp", address)
	if err != nil {
		return err
	}
	this.c = rawConn
	this.br = bufio.NewReader(rawConn)
	this.bw = bufio.NewWriter(rawConn)
	this.Opts.Handler.OnConnect(this)
	 this.serve()
	return nil

}

// Stop stops the conn.
func (this *YConn) Stop(mode StopMode) {
	this.once.Do(func() {
		if mode == StopImmediately {
			atomic.StoreInt32(&this.state, connStateStopped)
			this.c.Close()

			//close(c.sendBufList) // leave channel open, because other goroutine maybe use it in Send.
			close(this.closed)
		} else {
			atomic.StoreInt32(&this.state, connStateStopping)
			// c.RawConn.Close() 	// will close in sendLoop
			// close(c.sendBufList)
			close(this.closed)
			if mode == StopGracefullyAndWait {
				this.wg.Wait()
			}
		}
	})
}
func (this *YConn) serve() {
	tcpConn := this.c.(*net.TCPConn)
	tcpConn.SetNoDelay(this.Opts.NoDelay)
	tcpConn.SetKeepAlive(this.Opts.KeepAlive)
	if this.Opts.KeepAlivePeriod != 0 {
		tcpConn.SetKeepAlivePeriod(this.Opts.KeepAlivePeriod)
	}

	this.wg.Add(2)
	go this.sendLoop()

	this.recvLoop()

	this.Opts.Handler.OnClose(this)
}
func (this *YConn) IsStoped() bool {
	return atomic.LoadInt32(&this.state) != connStateNormal
}

func (this *YConn) recvLoop() {
	var tempDelay time.Duration
	maxDelay := 1 * time.Second
	defer func() {
		this.wg.Done()
	}()
	for {
		if this.Opts.ReadDeadline != 0 {
			this.c.SetReadDeadline(time.Now().Add(this.Opts.ReadDeadline))
		}

		pack, err := this.Opts.Handler.OnUnPack(this.br)
		if err != nil {
			this.Opts.Handler.OnUnpackErr(this, err)

			if nerr, ok := err.(net.Error); ok {
				if nerr.Timeout() {
					// timeout
				} else if nerr.Temporary() {
					if tempDelay == 0 {
						tempDelay = 5 * time.Millisecond
					} else {
						tempDelay *= 2
					}
					if tempDelay > maxDelay {
						tempDelay = maxDelay
					}

					time.Sleep(tempDelay)
					continue
				}
			}
			if !this.IsStoped() {
				this.Stop(StopImmediately)
			}
			return
		}else{
			this.Opts.Handler.OnRecv(this, pack)
		}
		tempDelay = 0

	}
}
func (this *YConn) sendLoop() {
	defer func() {

		this.wg.Done()
	}()
	for {
		if atomic.LoadInt32(&this.state) == connStateStopped {

			return
		}

		select {
		case pack, ok := <-this.sendChan:

			if !ok {
				return
			}
			_, err := pack.Write(this.bw)
			this.bw.Flush()
			this.Opts.Handler.OnSend(pack, err)

		case <-this.closed:
			if atomic.LoadInt32(&this.state) == connStateStopping {
				if len(this.sendChan) == 0 {
					atomic.SwapInt32(&this.state, connStateStopped)
					this.c.Close()

					return
				}
			}
		}
	}
}
