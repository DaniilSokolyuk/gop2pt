package webtorrent

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/DaniilSokolyuk/gop2pt"
	"github.com/gorilla/websocket"
	"github.com/pion/datachannel"
	"github.com/pion/webrtc/v3"
)

type TrackerClientStats struct {
	Dials                  int64
	ConvertedInboundConns  int64
	ConvertedOutboundConns int64
}

// Client represents the webtorrent client
type TrackerClient struct {
	Url      string
	PeerId   [20]byte
	InfoHash [20]byte
	OnConn   onDataChannelOpen
	Logger   gop2pt.Logger
	Dialer   *websocket.Dialer

	mu             sync.Mutex
	cond           sync.Cond
	outboundOffers map[string]outboundOffer // OfferID to outboundOffer
	wsConn         *websocket.Conn
	closed         bool
	stats          TrackerClientStats
	pingTicker     *time.Ticker
}

func (me *TrackerClient) Stats() TrackerClientStats {
	me.mu.Lock()
	defer me.mu.Unlock()
	return me.stats
}

func (me *TrackerClient) peerIdBinary() string {
	return binaryToJsonString(me.PeerId[:])
}

func (me *TrackerClient) infoHashBinary() string {
	return binaryToJsonString(me.InfoHash[:])
}

// outboundOffer represents an outstanding offer.
type outboundOffer struct {
	originalOffer  webrtc.SessionDescription
	peerConnection *wrappedPeerConnection
	dataChannel    *webrtc.DataChannel
	infoHash       [20]byte
}

type DataChannelContext struct {
	// Can these be obtained by just calling the relevant methods on peerConnection?
	Local, Remote webrtc.SessionDescription
	PeerID        string
	OfferId       string
	LocalOffered  bool
	// This is private as some methods might not be appropriate with data channel context.
	peerConnection *wrappedPeerConnection
}

func (me *DataChannelContext) GetSelectedIceCandidatePair() (*webrtc.ICECandidatePair, error) {
	return me.peerConnection.SCTP().Transport().ICETransport().GetSelectedCandidatePair()
}

type onDataChannelOpen func(_ datachannel.ReadWriteCloser, dcc DataChannelContext)

func (tc *TrackerClient) doWebsocket() error {
	metrics.Add("websocket dials", 1)
	tc.mu.Lock()
	tc.stats.Dials++
	tc.mu.Unlock()
	c, _, err := tc.Dialer.Dial(tc.Url, nil)
	if err != nil {
		return fmt.Errorf("dialing tracker: %w", err)
	}
	defer c.Close()
	tc.Logger.Debug("connected")
	tc.mu.Lock()
	tc.wsConn = c
	tc.cond.Broadcast()
	tc.mu.Unlock()
	tc.announceOffers()
	closeChan := make(chan struct{})
	go func() {
		for {
			select {
			case <-tc.pingTicker.C:
				tc.mu.Lock()
				err := c.WriteMessage(websocket.PingMessage, []byte{})
				tc.mu.Unlock()
				if err != nil {
					return
				}
			case <-closeChan:
				return

			}
		}
	}()
	err = tc.trackerReadLoop(tc.wsConn)
	close(closeChan)
	tc.mu.Lock()
	c.Close()
	tc.mu.Unlock()
	return err
}

// Finishes initialization and spawns the run routine, calling onStop when it completes with the
// result. We don't let the caller just spawn the runner directly, since then we can race against
// .Close to finish initialization.
func (tc *TrackerClient) Start(onStop func(error)) {
	tc.pingTicker = time.NewTicker(60 * time.Second)
	tc.cond.L = &tc.mu
	go func() {
		onStop(tc.run())
	}()
}

func (tc *TrackerClient) run() error {
	tc.mu.Lock()
	for !tc.closed {
		tc.mu.Unlock()
		err := tc.doWebsocket()
		tc.mu.Lock()
		tc.mu.Unlock()
		tc.Logger.Debug("websocket instance ended: %v", err)
		time.Sleep(time.Minute)
		tc.mu.Lock()
	}
	tc.mu.Unlock()
	return nil
}

func (tc *TrackerClient) Close() error {
	tc.mu.Lock()
	tc.closed = true
	if tc.wsConn != nil {
		tc.wsConn.Close()
	}
	tc.closeUnusedOffers()
	tc.pingTicker.Stop()
	tc.mu.Unlock()
	tc.cond.Broadcast()
	return nil
}

func (tc *TrackerClient) announceOffers() {
	// tc.Announce grabs a lock on tc.outboundOffers. It also handles the case where outboundOffers
	// is nil. Take ownership of outboundOffers here.
	tc.mu.Lock()
	offers := tc.outboundOffers
	tc.outboundOffers = nil
	tc.mu.Unlock()

	if offers == nil {
		return
	}

	// Iterate over our locally-owned offers, close any existing "invalid" ones from before the
	// socket reconnected, reannounce the infohash, adding it back into the tc.outboundOffers.
	tc.Logger.Error("reannouncing %d infohashes after restart", len(offers))
	for _, offer := range offers {
		// TODO: Capture the errors? Are we even in a position to do anything with them?
		offer.peerConnection.Close()
		// Use goroutine here to allow read loop to start and ensure the buffer drains.
		go tc.Announce()
	}
}

func (tc *TrackerClient) closeUnusedOffers() {
	for _, offer := range tc.outboundOffers {
		offer.peerConnection.Close()
	}
	tc.outboundOffers = nil
}

func (tc *TrackerClient) Announce() error {
	metrics.Add("outbound announces", 1)
	var randOfferId [20]byte
	_, err := rand.Read(randOfferId[:])
	if err != nil {
		return fmt.Errorf("generating offer_id bytes: %w", err)
	}
	offerIDBinary := binaryToJsonString(randOfferId[:])

	pc, dc, offer, err := newOffer()
	if err != nil {
		return fmt.Errorf("creating offer: %w", err)
	}

	req := AnnounceRequest{
		Numwant:    1, // If higher we need to create equal amount of offers.
		Uploaded:   0,
		Downloaded: 0,
		Left:       -1,
		Action:     "announce",
		InfoHash:   tc.infoHashBinary(),
		PeerID:     tc.peerIdBinary(),
		Offers: []Offer{{
			OfferID: offerIDBinary,
			Offer:   offer,
		}},
	}

	data, err := json.Marshal(req)
	if err != nil {
		pc.Close()
		return fmt.Errorf("marshalling request: %w", err)
	}

	tc.mu.Lock()
	defer tc.mu.Unlock()
	err = tc.writeMessage(data)
	if err != nil {
		pc.Close()
		return fmt.Errorf("write AnnounceRequest: %w", err)
	}
	if tc.outboundOffers == nil {
		tc.outboundOffers = make(map[string]outboundOffer)
	}
	tc.outboundOffers[offerIDBinary] = outboundOffer{
		peerConnection: pc,
		dataChannel:    dc,
		originalOffer:  offer,
		infoHash:       tc.InfoHash,
	}
	return nil
}

func (tc *TrackerClient) writeMessage(data []byte) error {
	for tc.wsConn == nil {
		if tc.closed {
			return fmt.Errorf("%T closed", tc)
		}
		tc.cond.Wait()
	}
	return tc.wsConn.WriteMessage(websocket.TextMessage, data)
}

func (tc *TrackerClient) trackerReadLoop(tracker *websocket.Conn) error {
	for {
		_, message, err := tracker.ReadMessage()
		if err != nil {
			return fmt.Errorf("read message error: %w", err)
		}
		// tc.Logger.WithDefaultLevel(log.Debug).Printf("received message from tracker: %q", message)

		var ar AnnounceResponse
		if err := json.Unmarshal(message, &ar); err != nil {
			tc.Logger.Error("error unmarshalling announce response: %v", err)
			continue
		}

		if ar.InfoHash != "" && ar.InfoHash != tc.infoHashBinary() {
			tc.Logger.Debug("ignoring websocket data from %s for %s (looking for %s: reused socket)",
				tc.Url, ar.InfoHash, tc.infoHashBinary())
			continue
		}

		if ar.PeerID != "" && ar.PeerID == tc.peerIdBinary() {
			// ignore offers/answers from this client
			continue
		}

		switch {
		case ar.Offer != nil:
			tc.handleOffer(*ar.Offer, ar.OfferID, ar.PeerID)
		case ar.Answer != nil:
			tc.handleAnswer(ar.OfferID, *ar.Answer, ar.PeerID)
		}
	}
}

func (tc *TrackerClient) handleOffer(
	offer webrtc.SessionDescription,
	offerId, peerId string) error {
	peerConnection, answer, err := newAnsweringPeerConnection(offer)
	if err != nil {
		return fmt.Errorf("write AnnounceResponse: %w", err)
	}
	response := AnnounceResponse{
		Action:   "announce",
		InfoHash: tc.infoHashBinary(),
		PeerID:   tc.peerIdBinary(),
		ToPeerID: peerId,
		Answer:   &answer,
		OfferID:  offerId,
	}
	data, err := json.Marshal(response)
	if err != nil {
		peerConnection.Close()
		return fmt.Errorf("marshalling response: %w", err)
	}
	tc.mu.Lock()
	defer tc.mu.Unlock()
	if err := tc.writeMessage(data); err != nil {
		peerConnection.Close()
		return fmt.Errorf("writing response: %w", err)
	}
	timer := time.AfterFunc(30*time.Second, func() {
		metrics.Add("answering peer connections timed out", 1)
		peerConnection.Close()
	})
	peerConnection.OnDataChannel(func(d *webrtc.DataChannel) {
		setDataChannelOnOpen(d, peerConnection, func(dc datachannel.ReadWriteCloser) {
			timer.Stop()
			metrics.Add("answering peer connection conversions", 1)
			tc.mu.Lock()
			tc.stats.ConvertedInboundConns++
			tc.mu.Unlock()
			tc.OnConn(dc, DataChannelContext{
				Local:          answer,
				Remote:         offer,
				OfferId:        offerId,
				LocalOffered:   false,
				PeerID:         peerId,
				peerConnection: peerConnection,
			})
		})
	})
	return nil
}

func (tc *TrackerClient) handleAnswer(offerId string, answer webrtc.SessionDescription, peerId string) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	offer, ok := tc.outboundOffers[offerId]
	if !ok {
		tc.Logger.Error("could not find offer for id %+q", offerId)
		return
	}
	// tc.Logger.WithDefaultLevel(log.Debug).Printf("offer %q got answer %v", offerId, answer)
	metrics.Add("outbound offers answered", 1)
	err := offer.setAnswer(answer, func(dc datachannel.ReadWriteCloser) {
		metrics.Add("outbound offers answered with datachannel", 1)
		tc.mu.Lock()
		tc.stats.ConvertedOutboundConns++
		tc.mu.Unlock()
		tc.OnConn(dc, DataChannelContext{
			Local:          offer.originalOffer,
			Remote:         answer,
			OfferId:        offerId,
			LocalOffered:   true,
			PeerID:         peerId,
			peerConnection: offer.peerConnection,
		})
	})
	if err != nil {
		tc.Logger.Error("error using outbound offer answer: %v", err)
		return
	}
	delete(tc.outboundOffers, offerId)
}
