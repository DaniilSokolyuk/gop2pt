package gop2pt

import (
	"net"
)

type webrtcListener struct {
	onConn chan *webrtcNetConn
	stopCh chan struct{}
	addr   net.Addr
}

func (w *webrtcListener) Accept() (net.Conn, error) {
	select {
	case conn := <-w.onConn:
		return conn, nil
	case <-w.stopCh:
		return nil, net.ErrClosed
	}
}

func (w *webrtcListener) Close() error {
	close(w.onConn)
	close(w.stopCh)

	return nil
}

func (w *webrtcListener) Addr() net.Addr {
	return w.addr
}
