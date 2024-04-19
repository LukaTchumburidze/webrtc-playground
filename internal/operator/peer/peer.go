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
	"webrtc-playground/internal/logger"
	"webrtc-playground/internal/model"
	"webrtc-playground/internal/operator/coordinator"
	"webrtc-playground/internal/worker"
)

const (
	BusyTimeout       = 20 * time.Second
	DelayAfterOffer   = 5 * time.Second
	TypeAppJson       = "application/json; charset=utf-8"
	UrlFormat         = "http://%s:%d%s"
	GoogleStunAddress = "stun:stun.l.google.com:19302"

	IceGetDelay  = 5 * time.Second
	IceSendDelay = 5 * time.Second
)

var (
	ErrCandidateIdNotSet     = errors.New("id is not set, can't send candidate")
	ErrNilWorker             = errors.New("nil worker has been passed to peer")
	ErrConnectionStateFailed = errors.New("peer Connection has gone to failed")
)

func (receiver *Peer) signalCandidates() error {
	if receiver.id == "" {
		return ErrCandidateIdNotSet
	}

	logger.Logger.WithField("cnt", len(receiver.pendingCandidates)).Info("Sending candidates to coordinator")

	receiver.candidatesMux.Lock()
	defer receiver.candidatesMux.Unlock()

	b, err := json.Marshal(receiver.pendingCandidates)
	if err != nil {
		return err
	}

	curPath := fmt.Sprintf("http://%v:%v/ice?id=%v", receiver.CoordinatorAddress, receiver.CoordinatorPort, receiver.id)

	resp, err := http.Post(curPath, // nolint:noctx
		"application/json; charset=utf-8", bytes.NewReader(b))
	if err != nil {
		return err
	}

	return resp.Body.Close()
}

func (receiver *Peer) getICECandidates() error {
	receiver.candidatesMux.Lock()
	defer receiver.candidatesMux.Unlock()

	logger.Logger.WithField("address", fmt.Sprintf("%v:%v", receiver.CoordinatorAddress, receiver.CoordinatorPort)).Info("Started getting ICEInit Candidates from coordinator")
	for try := 0; try < 1; try++ {
		curPath := fmt.Sprintf("http://%v:%v/ice?id=%v", receiver.CoordinatorAddress, receiver.CoordinatorPort, receiver.id)
		resp, err := http.Get(curPath)

		if err != nil {
			return err
		}

		var iceCandidates []webrtc.ICECandidateInit
		if err := json.NewDecoder(resp.Body).Decode(&iceCandidates); err != nil {
			return err
		}
		resp.Body.Close()

		for _, iceCandidate := range iceCandidates {
			err = receiver.PeerConnection.AddICECandidate(iceCandidate)
			if err != nil {
				return err
			}
		}

		logger.Logger.WithField("cnt", len(iceCandidates)).Info("Received candidates from coordinator")
	}

	logger.Logger.Info("Finished getting ICEInit Candidates from coordinator")
	return nil
}

type Peer struct {
	CoordinatorAddress string
	CoordinatorPort    uint16
	PeerConnection     *webrtc.PeerConnection
	waitChannel        chan error
	pendingCandidates  []*webrtc.ICECandidateInit
	candidatesMux      sync.Mutex
	sentMsgCnt         int
	receivedMsgCnt     int
	id                 string

	worker *worker.Worker
}

func New(coordinatorAddress string, coordinatorPort uint16, worker *worker.Worker) (peer *Peer, err error) {
	if worker == nil {
		return nil, ErrNilWorker
	}

	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{GoogleStunAddress},
			},
		},
	}
	peerConnection, err := webrtc.NewPeerConnection(config)
	if err != nil {
		return
	}

	peer = &Peer{
		CoordinatorAddress: coordinatorAddress,
		CoordinatorPort:    coordinatorPort,
		PeerConnection:     peerConnection,
		waitChannel:        make(chan error),
		pendingCandidates:  make([]*webrtc.ICECandidateInit, 0),
		worker:             worker,
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

func (receiver *Peer) sendOfferSDP() error {
	// SEND offer role
	logger.Logger.Info("Sending Offer SDP")

	offer, err := receiver.PeerConnection.CreateOffer(nil)
	if err != nil {
		return err
	}

	payload, err := json.Marshal(offer)
	if err != nil {
		return err
	}

	curPath := fmt.Sprintf(UrlFormat, receiver.CoordinatorAddress, receiver.CoordinatorPort, coordinator.HttpSDPPath)
	_, err = http.Post(curPath,
		TypeAppJson,
		bytes.NewReader(payload))
	if err != nil {
		return err
	}

	if err = receiver.PeerConnection.SetLocalDescription(offer); err != nil {
		return err
	}

	return nil
}

func (receiver *Peer) getAnswerSDPID() (string, error) {
	// RECEIVE answer connection
	logger.Logger.Info("Getting answer SDP")

	curPath := fmt.Sprintf(UrlFormat, receiver.CoordinatorAddress, receiver.CoordinatorPort, coordinator.HttpSDPAnswerPath)
	resp, err := http.Get(curPath)
	if err != nil {
		return "", err
	}
	var answerPayload model.SDPPayload
	err = json.NewDecoder(resp.Body).Decode(&answerPayload)
	if err := resp.Body.Close(); err != nil {
		return "", err
	}
	err = receiver.PeerConnection.SetRemoteDescription(*answerPayload.SDP)
	if err != nil {
		return "", err
	}

	return answerPayload.ID, nil
}

func (receiver *Peer) setupDataChannel() error {
	dataChannel, err := receiver.PeerConnection.CreateDataChannel(coordinator.RandSeq(5), nil)
	if err != nil {
		return err
	}
	logger.Logger.WithField("label", dataChannel.Label()).Info("Created DataChannel")

	dataChannel.OnOpen(func() {
		var err error
		for err != nil && !errors.Is(err, worker.ErrFinish) {
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

func (receiver *Peer) resolveOffer() error {
	if err := receiver.sendOfferSDP(); err != nil {
		return err
	}

	logger.Logger.WithField("duration", DelayAfterOffer).Info("Sleeping after offer")
	time.Sleep(DelayAfterOffer)

	id, err := receiver.getAnswerSDPID()
	if err != nil {
		return err
	}

	receiver.id = id

	return nil
}

func (receiver *Peer) getOfferSDPID() (string, error) {
	curPath := fmt.Sprintf(UrlFormat, receiver.CoordinatorAddress, receiver.CoordinatorPort, coordinator.HttpSDPPath)

	resp, err := http.Get(curPath)
	if err != nil {
		return "", err
	}

	var offerPayload model.SDPPayload
	err = json.NewDecoder(resp.Body).Decode(&offerPayload)
	if err := resp.Body.Close(); err != nil {
		return "", err
	}
	err = receiver.PeerConnection.SetRemoteDescription(*offerPayload.SDP)
	if err != nil {
		return "", err
	}

	return offerPayload.ID, nil
}

func (receiver *Peer) sendAnswerSDP() error {
	answerSdp, err := receiver.PeerConnection.CreateAnswer(nil)
	if err != nil {
		return err
	}

	err = receiver.PeerConnection.SetLocalDescription(answerSdp)
	if err != nil {
		return err
	}

	payload, err := json.Marshal(answerSdp)
	if err != nil {
		return err
	}

	logger.Logger.Info("Sending answer SDP")
	curPath := fmt.Sprintf(UrlFormat, receiver.CoordinatorAddress, receiver.CoordinatorPort, coordinator.HttpSDPAnswerPath)
	_, err = http.Post(curPath,
		TypeAppJson,
		bytes.NewReader(payload))
	if err != nil {
		return err
	}

	return nil
}

func (receiver *Peer) resolveAnswer() error {
	logger.Logger.Info("Getting offer SDP")

	id, err := receiver.getOfferSDPID()
	if err != nil {
		return err
	}

	if err = receiver.sendAnswerSDP(); err != nil {
		return err
	}
	receiver.id = id
	return nil
}

func (receiver *Peer) resolveStatus(status model.RoleStatus) error {
	if err := receiver.setupDataChannel(); err != nil {
		return nil
	}

	switch status {
	case coordinator.RoleOffer:
		err := receiver.resolveOffer()
		if err != nil {
			return err
		}

		curPath := fmt.Sprintf(UrlFormat, receiver.CoordinatorAddress, receiver.CoordinatorPort, coordinator.HttpRegisterDonePath)
		_, err = http.Post(curPath,
			TypeAppJson,
			bytes.NewReader([]byte{}))
		if err != nil {
			return err
		}

		logger.Logger.Info("Registration done")
	case coordinator.RoleAnswer:
		err := receiver.resolveAnswer()
		if err != nil {
			return err
		}
	}

	time.Sleep(IceSendDelay)
	err := receiver.signalCandidates()
	if err != nil {
		return nil
	}
	time.Sleep(IceGetDelay)
	err = receiver.getICECandidates()
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

	status := coordinator.StatusBusy

	for status == coordinator.StatusBusy {
		curPath := fmt.Sprintf(UrlFormat, receiver.CoordinatorAddress, receiver.CoordinatorPort, coordinator.HttpRegisterPath)
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
			"duration": BusyTimeout,
		}).Info("status received, sleeping")
		time.Sleep(BusyTimeout)
	}

	err := receiver.resolveStatus(status)

	if err != nil {
		return err
	}

	logger.Logger.WithField("status", status).Info("Peer finished SDP exchange")
	return nil
}
