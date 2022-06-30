package gop2pt

import (
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/datachannel"

	dslog "github.com/DaniilSokolyuk/gop2pt/log"
	"github.com/DaniilSokolyuk/gop2pt/utils"
	"github.com/DaniilSokolyuk/gop2pt/webtorrent"
)

var (
	defaultAnnounceInterval = time.Second * 50
	defaultNumWant          = 5
)

type ProxyFunc func(*http.Request) (*url.URL, error)

type Option func(*P2PT)

func AnnounceInterval(announceInterval time.Duration) Option {
	return func(p *P2PT) {
		p.announceInterval = announceInterval
	}
}

func NumWant(numWant int) Option {
	return func(p *P2PT) {
		p.numWant = numWant
	}
}

func WithLogger(logger dslog.Logger) Option {
	return func(p *P2PT) {
		p.logger = logger
	}
}

func WithProxy(proxy ProxyFunc) Option {
	return func(p *P2PT) {
		p.proxy = proxy
	}
}

type defaultLog struct {
	*log.Logger
}

func DefaultLogger() *defaultLog {
	return &defaultLog{Logger: log.New(os.Stderr, "p2pt ", log.LstdFlags)}
}

func (l *defaultLog) Error(f string, v ...interface{}) {
	l.Printf("ERROR: "+f, v...)
}

func (l *defaultLog) Warn(f string, v ...interface{}) {
	l.Printf("WARNING: "+f, v...)
}

func (l *defaultLog) Info(f string, v ...interface{}) {
	l.Printf("INFO: "+f, v...)
}

func (l *defaultLog) Debug(f string, v ...interface{}) {
	l.Printf("DEBUG: "+f, v...)
}

type refCountedWebtorrentTrackerClient struct {
	webtorrent.TrackerClient
	refCount int
}

type P2PT struct {
	peerIDBinary     string
	infoHashBinary   string
	announceURLs     []string
	announceInterval time.Duration
	numWant          int
	logger           dslog.Logger
	proxy            ProxyFunc

	mu      sync.Mutex
	clients map[string]*refCountedWebtorrentTrackerClient
}

func New(identifier string, announceURLs []string, opts ...Option) *P2PT {
	p2pt := &P2PT{
		peerIDBinary:     utils.MakePeerID(),
		infoHashBinary:   utils.MakeInfoHash(identifier),
		announceURLs:     announceURLs,
		announceInterval: defaultAnnounceInterval,
		numWant:          defaultNumWant,
		logger:           DefaultLogger(),
		proxy:            nil,

		clients: make(map[string]*refCountedWebtorrentTrackerClient),
	}

	for _, o := range opts {
		o(p2pt)
	}

	return p2pt
}

func (p *P2PT) Start() (net.Listener, error) {
	listener := &webrtcListener{
		addr:   webrtcNetAddr{peerIDBinary: p.peerIDBinary},
		onConn: make(chan *webrtcNetConn),
		stopCh: make(chan struct{}),
	}

	for _, url := range p.announceURLs {
		p.connectTracker(url, func(ch datachannel.ReadWriteCloser, dcc webtorrent.DataChannelContext) {
			conn := &webrtcNetConn{
				ReadWriteCloser:    ch,
				DataChannelContext: dcc,
			}

			p.logger.Debug("new connection (id: %s, local offered: %t, tracker: %s", conn.RemoteAddr(), dcc.LocalOffered, url)

			listener.onConn <- conn
		})
	}

	go func() {
		ticker := time.NewTicker(p.announceInterval)

		for {
			select {
			case <-ticker.C:
				for _, cl := range p.clients {
					cl.Announce()
				}
			case <-listener.stopCh:
				ticker.Stop()
				for _, value := range p.clients {
					value.TrackerClient.Close()
				}
				return
			}
		}
	}()

	return listener, nil
}

func (p *P2PT) connectTracker(
	url string,
	onConn func(ch datachannel.ReadWriteCloser, dcc webtorrent.DataChannelContext)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	value, ok := p.clients[url]
	if !ok {
		dialer := &websocket.Dialer{Proxy: p.proxy, HandshakeTimeout: websocket.DefaultDialer.HandshakeTimeout}
		value = &refCountedWebtorrentTrackerClient{
			TrackerClient: webtorrent.TrackerClient{
				NumWant:  p.numWant,
				Url:      url,
				PeerId:   p.peerIDBinary,
				InfoHash: p.infoHashBinary,
				OnConn:   onConn,
				Logger:   p.logger,
				Dialer:   dialer,
			},
		}
		value.TrackerClient.Start(func(err error) {
			if err != nil {
				p.logger.Error("error running tracker client for %q: %v", url, err)
			}
		})

		value.TrackerClient.Announce()

		p.clients[url] = value
	}
}
