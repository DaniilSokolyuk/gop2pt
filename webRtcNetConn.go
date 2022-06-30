package gop2pt

import (
	"github.com/mr-tron/base58"
	"net"
	"time"

	"github.com/DaniilSokolyuk/gop2pt/webtorrent"
	"github.com/pion/datachannel"
)

const webrtcNetwork = "webrtc"

type webrtcNetConn struct {
	datachannel.ReadWriteCloser
	webtorrent.DataChannelContext
}

func (c webrtcNetConn) LocalAddr() net.Addr {
	return webrtcNetAddr{
		peerIDBinary: c.PeerID,
	}
}

func (c webrtcNetConn) RemoteAddr() net.Addr {
	return webrtcNetAddr{
		peerIDBinary: c.PeerID,
	}
}

// Do we need these for WebRTC connections exposed as net.Conns? Can we set them somewhere inside
// PeerConnection or on the channel or some transport?

func (c webrtcNetConn) SetDeadline(_ time.Time) error {
	return nil
}

func (c webrtcNetConn) SetReadDeadline(_ time.Time) error {
	return nil
}

func (c webrtcNetConn) SetWriteDeadline(_ time.Time) error {
	return nil
}

type webrtcNetAddr struct {
	peerIDBinary string
}

func (webrtcNetAddr) Network() string {
	return webrtcNetwork
}

func (a webrtcNetAddr) String() string {
	return base58.Encode([]byte(a.peerIDBinary))
}
