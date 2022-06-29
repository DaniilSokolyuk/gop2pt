package main

import (
	"crypto/rand"
	"crypto/sha1"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/datachannel"

	"github.com/DaniilSokolyuk/gop2pt/webtorrent"
)

func main() {
	infoHash := makeInfoHash("p2chatgeneral")
	peerID := makePeerID()

	tt := websocketTrackers{
		PeerId:   peerID,
		InfoHash: infoHash,
		OnConn: func(closer datachannel.ReadWriteCloser, context webtorrent.DataChannelContext) {
			go onConn(closer, context)
		},
		mu:      sync.Mutex{},
		clients: nil,
		Proxy:   nil,
	}

	test, f := tt.Get("wss://tracker.btorrent.xyz/announce")
	test.Announce()

	_ = test
	_ = f

	time.Sleep(time.Second * 500)
}

func makePeerID() [20]byte {
	peerIDB := make([]byte, 20)
	rand.Read(peerIDB)

	var peerID [20]byte
	copy(peerID[:], peerIDB)

	return peerID
}

func makeInfoHash(s string) [20]byte {
	var infoHash [20]byte
	hash := sha1.New()
	hash.Write([]byte(s))
	copy(infoHash[:], hash.Sum(nil))

	return infoHash
}

func onConn(ch datachannel.ReadWriteCloser, ctx webtorrent.DataChannelContext) {
	tt, _ := ctx.GetSelectedIceCandidatePair()
	l, r := fmt.Sprintf("%s:%d", tt.Local.Address, tt.Local.Port), fmt.Sprintf("%s:%d", tt.Remote.Address, tt.Remote.Port)

	fmt.Println("onConn", l, r, ctx.LocalOffered, ctx.PeerID)
	for {
		bytes := make([]byte, 16000)
		i, _ := ch.Read(bytes)
		if i == 0 {
			fmt.Println("disconected")
			ch.Close()
			break
		}

		fmt.Println("MESSAGE", i, string(bytes[:i]), ctx.LocalOffered, "l:", l, "r:", r, ctx.PeerID)
	}
}

type refCountedWebtorrentTrackerClient struct {
	webtorrent.TrackerClient
	refCount int
}

type ProxyFunc func(*http.Request) (*url.URL, error)

type websocketTrackers struct {
	PeerId   [20]byte
	InfoHash [20]byte
	OnConn   func(datachannel.ReadWriteCloser, webtorrent.DataChannelContext)
	mu       sync.Mutex
	clients  map[string]*refCountedWebtorrentTrackerClient
	Proxy    ProxyFunc
}

func (me *websocketTrackers) Get(url string) (*webtorrent.TrackerClient, func()) {
	me.mu.Lock()
	defer me.mu.Unlock()
	value, ok := me.clients[url]
	if !ok {
		dialer := &websocket.Dialer{Proxy: me.Proxy, HandshakeTimeout: websocket.DefaultDialer.HandshakeTimeout}
		value = &refCountedWebtorrentTrackerClient{
			TrackerClient: webtorrent.TrackerClient{
				Dialer:   dialer,
				Url:      url,
				PeerId:   me.PeerId,
				InfoHash: me.InfoHash,
				OnConn:   me.OnConn,
				Logger:   logger,
			},
		}
		value.TrackerClient.Start(func(err error) {
			if err != nil {
				logger.Printf("error running tracker client for %q: %v", url, err)
			}
		})
		if me.clients == nil {
			me.clients = make(map[string]*refCountedWebtorrentTrackerClient)
		}
		me.clients[url] = value
	}
	value.refCount++
	return &value.TrackerClient, func() {
		me.mu.Lock()
		defer me.mu.Unlock()
		value.refCount--
		if value.refCount == 0 {
			value.TrackerClient.Close()
			delete(me.clients, url)
		}
	}
}
