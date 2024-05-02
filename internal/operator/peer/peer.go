package peer

import (
	"errors"
	"github.com/pion/webrtc/v3"
	"sync"
	"time"
	"webrtc-playground/config"
	"webrtc-playground/internal/logger"
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

type (
	peerConnector interface {
		connectPeers(peer *Peer) error
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
