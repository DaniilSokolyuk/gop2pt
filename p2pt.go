package gop2pt

import (
	"time"
)

var (
	defaultAnnounceInterval = time.Second * 20
	defaultNumWant          = 10
)

func New(identifier string, announceURLs []string, opts ...Option) *P2PT {
	p2pt := &P2PT{
		identifier:       identifier,
		announceURLs:     announceURLs,
		announceInterval: defaultAnnounceInterval,
		numWant:          defaultNumWant,
		logger:           defaultLogger(),
	}

	for _, o := range opts {
		o(p2pt)
	}

	return p2pt
}

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

func WithLogger(logger Logger) Option {
	return func(p *P2PT) {
		p.logger = logger
	}
}

type P2PT struct {
	identifier       string
	announceURLs     []string
	announceInterval time.Duration
	numWant          int
	logger           Logger
}

func (p *P2PT) Start() {

}
