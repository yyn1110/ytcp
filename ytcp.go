package ytcp

import (
	"io"
	"time"
	"fmt"
)

type PACK interface {
	fmt.Stringer
	//Pack
	ReadPack(reader io.Reader, aesKey string) (pack PACK, err error)
	//UnPack
	Write(writer io.Writer) (n int, err error)
}

var (
	// DefaultRecvBufSize is the default size of recv buf.
	DefaultRecvBufSize = 4 << 10 // 4k
	// DefaultSendBufListLen is the default length of send buf list.
	DefaultSendBufListLen = 1 << 10 // 1k

)

type YConnHandler interface {
	//是否接受连接
	CanAccept(conn *YConn) error
	//服务端收到客户端后回调
	OnAccept(conn *YConn)
	// 客户端连接到服务端后回调
	OnConnect(conn *YConn)
	//发送一个包后回调
	OnSend(pack PACK, err error)
	//拆包
	OnUnPack(r io.Reader) (PACK, error)
	// 拆包后回调
	OnUnpackErr(conn *YConn, e error)
	// 收到包后回调
	OnRecv(conn *YConn, pack PACK)
	// conn close回调
	OnClose(conn *YConn)
}

type Options struct {
	Handler YConnHandler

	RecvBufSize     int           // default is DefaultRecvBufSize if you don't set.
	SendBufListLen  int           // default is DefaultSendBufListLen if you don't set.

	NoDelay         bool          // default is true
	KeepAlive       bool          // default is false
	KeepAlivePeriod time.Duration // default is 0, mean use system setting.
	ReadDeadline    time.Duration // default is 0, means Read will not time out.
	WriteDeadline   time.Duration // default is 0, means Write will not time out.
}

func NewOpts(h YConnHandler) *Options {
	if h == nil {
		panic("ytcp.NewOpts: nil handler or protocol")
	}
	return &Options{
		Handler:        h,
		RecvBufSize:    DefaultRecvBufSize,
		SendBufListLen: DefaultSendBufListLen,
		NoDelay:        true,
		KeepAlive:      true,
	}
}
