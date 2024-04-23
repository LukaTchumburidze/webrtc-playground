package peer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/pion/webrtc/v3"
	"github.com/sirupsen/logrus"
	"net/http"
	"sync"
	"time"
	"webrtc-playground/config"
	"webrtc-playground/internal/logger"
	"webrtc-playground/internal/model"
	"webrtc-playground/internal/operator/coordinator"
	"webrtc-playground/internal/worker"
)

const (
	busyTimeout          = 20 * time.Second
	delayAfterOffer      = 5 * time.Second
	TypeAppJson          = "application/json; charset=utf-8"
	coordinatorUrlFormat = "http://%s:%d%s"
	googleStunAddress    = "stun:stun.l.google.com:19302"

	iceGetDelay  = 5 * time.Second
	iceSendDelay = 5 * time.Second
)

var (
	ErrCandidateIdNotSet     = errors.New("id is not set, can't send candidate")
	ErrNilWorker             = errors.New("nil worker has been passed to peer")
	ErrConnectionStateFailed = errors.New("peer Connection has gone to failed")
	ErrCantGetICECandidates  = errors.New("can't get ICE candidates")
)

func (c httpPeerConnector) signalCandidates(id string, pendingCandidates []*webrtc.ICECandidateInit) error {
	if id == "" {
		return ErrCandidateIdNotSet
	}

	logger.Logger.WithField("cnt", len(pendingCandidates)).Info("Sending candidates to coordinator")

	b, err := json.Marshal(pendingCandidates)
	if err != nil {
		return err
	}

	curPath := fmt.Sprintf("http://%v:%v/ice?id=%v", c.coordinatorAddress, c.coordinatorPort, id)

	resp, err := http.Post(curPath, // nolint:noctx
		TypeAppJson, bytes.NewReader(b))
	if err != nil {
		return err
	}

	return resp.Body.Close()
}

func (c httpPeerConnector) getICECandidates(id string) ([]*webrtc.ICECandidateInit, error) {
	logger.Logger.WithField("address", fmt.Sprintf("%v:%v", c.coordinatorAddress, c.coordinatorPort)).Info("Started getting ICEInit Candidates from coordinator")
	for try := 0; try < 1; try++ {
		curPath := fmt.Sprintf("http://%v:%v/ice?id=%v", c.coordinatorAddress, c.coordinatorPort, id)
		resp, err := http.Get(curPath)

		if err != nil {
			return nil, err
		}

		var iceCandidates []*webrtc.ICECandidateInit
		if err := json.NewDecoder(resp.Body).Decode(&iceCandidates); err != nil {
			return nil, err
		}
		resp.Body.Close()

		logger.Logger.WithField("cnt", len(iceCandidates)).Info("Received candidates from coordinator")
		return iceCandidates, nil
	}

	logger.Logger.Info("Finished getting ICEInit Candidates from coordinator")
	return nil, ErrCantGetICECandidates
}

type (
	peerConnector interface {
		connectPeers(peer *Peer) error
	}

	httpPeerConnector struct {
		coordinatorAddress string
		coordinatorPort    uint16
	}
	ircPeerConnector struct {
		serverPath string
		channel    string
	}

	Peer struct {
		connector         peerConnector
		PeerConnection    *webrtc.PeerConnection
		waitChannel       chan error
		pendingCandidates []*webrtc.ICECandidateInit
		candidatesMux     sync.Mutex
		sentMsgCnt        int
		receivedMsgCnt    int
		id                string

		worker *worker.Worker
	}
)

func (c httpPeerConnector) connectPeers(peer *Peer) error {
	status := coordinator.StatusBusy

	for status == coordinator.StatusBusy {
		curPath := fmt.Sprintf(coordinatorUrlFormat, c.coordinatorAddress, c.coordinatorPort, coordinator.HttpRegisterPath)
		resp, err := http.Post(curPath,
			TypeAppJson,
			bytes.NewReader([]byte{}))
		if err != nil {
			return err
		}
		err = json.NewDecoder(resp.Body).Decode(&status)
		if err != nil {
			return err
		}

		logger.Logger.WithFields(logrus.Fields{
			"status":   status,
			"duration": busyTimeout,
		}).Info("status received, sleeping")
		time.Sleep(busyTimeout)
	}

	err := c.resolveStatus(status, peer)
	if err != nil {
		return err
	}

	logger.Logger.WithField("status", status).Info("Peer finished SDP exchange")
	return nil
}

func (c ircPeerConnector) connectPeers(peer *Peer) error {
	//TODO implement me
	panic("implement me")
}

func NewPeer(cmdConfig config.PeerCmdConfig, worker *worker.Worker) (peer *Peer, err error) {
	if worker == nil {
		return nil, ErrNilWorker
	}

	webrtcCfg := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{googleStunAddress},
			},
		},
	}
	peerConnection, err := webrtc.NewPeerConnection(webrtcCfg)
	if err != nil {
		return
	}

	// Check which connector to use
	var connector peerConnector
	if cmdConfig.SDPExchangeMethod == "http" {
		connector = httpPeerConnector{
			coordinatorAddress: cmdConfig.CoordinatorAddress,
			coordinatorPort:    cmdConfig.CoordinatorPort,
		}
	} else {
		connector = ircPeerConnector{
			// TODO: initialize this as per irc cmd config
		}
	}

	peer = &Peer{
		connector:         connector,
		PeerConnection:    peerConnection,
		waitChannel:       make(chan error),
		pendingCandidates: make([]*webrtc.ICECandidateInit, 0),
		worker:            worker,
	}

	// Set the handler for Peer connection state
	// This will notify you when the peer has connected/disconnected
	peerConnection.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		logger.Logger.WithField("state", s.String()).Info("Peer Connection State has changed")

		if s == webrtc.PeerConnectionStateFailed {
			// Await until PeerConnection has had no network activity for 30 seconds or another failure. It may be reconnected using an ICEInit Restart.
			// Use webrtc.PeerConnectionStateDisconnected if you are interested in detecting faster timeout.
			// Note that the PeerConnection may come back from PeerConnectionStateDisconnected.

			peer.stop(ErrConnectionStateFailed)
		}

		//TODO: Currently connection state disconnected is being regarded as valid exit, don't think this is correct
		if s == webrtc.PeerConnectionStateClosed || s == webrtc.PeerConnectionStateDisconnected {
			logger.Logger.Info("Valid terminal state change, exiting")
			peer.Stop()
		}
	})

	return
}

func (receiver *Peer) Await() error {
	logger.Logger.Info("Waiting to stop")
	select {
	case err := <-receiver.waitChannel:
		return err
	}
}

func (receiver *Peer) stop(err error) {
	receiver.waitChannel <- err
}

func (receiver *Peer) Stop() {
	receiver.stop(nil)
}

func (c httpPeerConnector) sendOfferSDP(offerSDP *webrtc.SessionDescription) error {
	// SEND offer role
	logger.Logger.Info("Sending Offer SDP")

	payload, err := json.Marshal(offerSDP)
	if err != nil {
		return err
	}

	curPath := fmt.Sprintf(coordinatorUrlFormat, c.coordinatorAddress, c.coordinatorPort, coordinator.HttpSDPOfferPath)
	_, err = http.Post(curPath,
		TypeAppJson,
		bytes.NewReader(payload))
	return err
}

func (c httpPeerConnector) getAnswerSDPPayload() (*model.SDPPayload, error) {
	// RECEIVE answer connection
	logger.Logger.Info("Getting answer SDP")

	curPath := fmt.Sprintf(coordinatorUrlFormat, c.coordinatorAddress, c.coordinatorPort, coordinator.HttpSDPAnswerPath)
	resp, err := http.Get(curPath)
	if err != nil {
		return nil, err
	}
	var answerPayload model.SDPPayload
	err = json.NewDecoder(resp.Body).Decode(&answerPayload)
	if err := resp.Body.Close(); err != nil {
		return nil, err
	}

	return &answerPayload, nil
}

func (receiver *Peer) setupDataChannel() error {
	dataChannel, err := receiver.PeerConnection.CreateDataChannel(coordinator.RandSeq(5), nil)
	if err != nil {
		return err
	}
	logger.Logger.WithField("label", dataChannel.Label()).Info("Created DataChannel")

	dataChannel.OnOpen(func() {
		var err error
		for err == nil {
			b, err := (*receiver.worker).ProducePayload()
			if err != nil {
				if errors.Is(err, worker.ErrFinish) {
					break
				} else {
					// Could not identify error, we should panic
					panic(err)
				}
			}
			err = dataChannel.Send(b)
			if err != nil {
				// Could not send for some reason, we should panic
				panic(err)
			}
		}
		logger.Logger.Info("Worker finished producing payload, closing DataChannel")
		dataChannel.Close()

		err = receiver.PeerConnection.Close()
		if err != nil {
			logger.Logger.WithError(err).Error("error on peer connection close")
		}
	})

	// Current version basically is just one DataChannel, if it gets closed, peers should finish work
	dataChannel.OnClose(func() {
		logger.Logger.Info("DataChannel was closed, closing peer connection")
		receiver.PeerConnection.Close()
	})

	receiver.PeerConnection.OnDataChannel(func(channel *webrtc.DataChannel) {
		logger.Logger.WithField("label", channel.Label()).Info("OnDataChannel")
		// Register text message handling
		channel.OnMessage(func(msg webrtc.DataChannelMessage) {
			b := msg.Data
			err = (*receiver.worker).ConsumePayload(b)
			if err != nil {
				// Could not consume payload properly, since there is no further mechanism we should panic
				panic(err)
			}
		})
	})

	return nil
}

func (c httpPeerConnector) resolveOffer(offerSDP *webrtc.SessionDescription) (*model.SDPPayload, error) {
	if err := c.sendOfferSDP(offerSDP); err != nil {
		return nil, err
	}

	logger.Logger.WithField("duration", delayAfterOffer).Info("Sleeping after offer")
	time.Sleep(delayAfterOffer)

	sdpPayload, err := c.getAnswerSDPPayload()
	if err != nil {
		return nil, err
	}

	return sdpPayload, nil
}

func (c httpPeerConnector) getOfferSDP() (*model.SDPPayload, error) {
	curPath := fmt.Sprintf(coordinatorUrlFormat, c.coordinatorAddress, c.coordinatorPort, coordinator.HttpSDPOfferPath)

	resp, err := http.Get(curPath)
	if err != nil {
		return nil, err
	}

	var offerPayload model.SDPPayload
	err = json.NewDecoder(resp.Body).Decode(&offerPayload)
	if err := resp.Body.Close(); err != nil {
		return nil, err
	}

	return &offerPayload, nil
}

func (c httpPeerConnector) sendAnswerSDP(answerSDP *webrtc.SessionDescription) error {
	payload, err := json.Marshal(answerSDP)
	if err != nil {
		return err
	}

	logger.Logger.Info("Sending answer SDP")
	curPath := fmt.Sprintf(coordinatorUrlFormat, c.coordinatorAddress, c.coordinatorPort, coordinator.HttpSDPAnswerPath)
	_, err = http.Post(curPath,
		TypeAppJson,
		bytes.NewReader(payload))
	if err != nil {
		return err
	}

	return nil
}

func (c httpPeerConnector) resolveAnswer(peer *Peer) error {
	logger.Logger.Info("Getting offer SDP")

	offerPayload, err := c.getOfferSDP()
	if err != nil {
		return err
	}
	err = peer.PeerConnection.SetRemoteDescription(*offerPayload.SDP)
	if err != nil {
		return err
	}

	answerSDP, err := peer.PeerConnection.CreateAnswer(nil)
	if err != nil {
		return err
	}
	err = peer.PeerConnection.SetLocalDescription(answerSDP)
	if err != nil {
		return err
	}
	if err = c.sendAnswerSDP(&answerSDP); err != nil {
		return err
	}
	peer.id = offerPayload.ID
	return nil
}

func (c httpPeerConnector) resolveStatus(status model.RoleStatus, peer *Peer) error {
	switch status {
	case coordinator.RoleOffer:
		offerSDP, err := peer.PeerConnection.CreateOffer(nil)
		if err != nil {
			return err
		}

		err = peer.PeerConnection.SetLocalDescription(offerSDP)
		if err != nil {
			return err
		}
		sdpAnswerPayload, err := c.resolveOffer(&offerSDP)
		if err != nil {
			return err
		}
		err = peer.PeerConnection.SetRemoteDescription(*sdpAnswerPayload.SDP)
		if err != nil {
			return err
		}
		peer.id = sdpAnswerPayload.ID

		curPath := fmt.Sprintf(coordinatorUrlFormat, c.coordinatorAddress, c.coordinatorPort, coordinator.HttpRegisterDonePath)
		_, err = http.Post(curPath,
			TypeAppJson,
			bytes.NewReader([]byte{}))
		if err != nil {
			return err
		}

		logger.Logger.Info("Registration done")
	case coordinator.RoleAnswer:
		err := c.resolveAnswer(peer)
		if err != nil {
			return err
		}
	}

	logger.Logger.WithField("id", peer.id).Info("Peer got assigned id")

	time.Sleep(iceSendDelay)
	peer.candidatesMux.Lock()
	err := c.signalCandidates(peer.id, peer.pendingCandidates)
	peer.candidatesMux.Unlock()
	if err != nil {
		return nil
	}
	time.Sleep(iceGetDelay)

	peer.candidatesMux.Lock()
	iceCandidates, err := c.getICECandidates(peer.id)
	for _, iceCandidate := range iceCandidates {
		err = peer.PeerConnection.AddICECandidate(*iceCandidate)
		if err != nil {
			return err
		}
	}
	peer.candidatesMux.Unlock()

	if err != nil {
		return nil
	}

	return nil
}

func (receiver *Peer) InitConnection() error {
	receiver.PeerConnection.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}

		receiver.candidatesMux.Lock()
		defer receiver.candidatesMux.Unlock()

		logger.Logger.WithField("candidate", c.String()).Info("Adding candidate")
		receiver.pendingCandidates = append(receiver.pendingCandidates, &webrtc.ICECandidateInit{Candidate: c.ToJSON().Candidate})
	})

	if err := receiver.setupDataChannel(); err != nil {
		return err
	}
	return receiver.connector.connectPeers(receiver)
}
