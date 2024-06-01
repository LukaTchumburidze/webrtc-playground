package peer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/pion/webrtc/v3"
	"github.com/sirupsen/logrus"
	"net/http"
	"time"
	"webrtc-playground/internal/logger"
	"webrtc-playground/internal/model"
	"webrtc-playground/internal/operator/coordinator"
)

const nOfICECandidateGetTres = 1

type httpPeerConnector struct {
	coordinatorAddress string
	coordinatorPort    uint16
}

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
	for try := 0; try < nOfICECandidateGetTres; try++ {
		curPath := fmt.Sprintf("http://%v:%v/ice?id=%v", c.coordinatorAddress, c.coordinatorPort, id)
		resp, err := http.Get(curPath)

		if err != nil {
			return nil, err
		}

		var iceCandidates []*webrtc.ICECandidateInit
		if err := json.NewDecoder(resp.Body).Decode(&iceCandidates); err != nil {
			return nil, err
		}
		if err := resp.Body.Close(); err != nil {
			return nil, err
		}

		logger.Logger.WithField("cnt", len(iceCandidates)).Info("Received candidates from coordinator")
		if len(iceCandidates) != 0 {
			return iceCandidates, nil
		}

		logger.Logger.WithField("tries", try).Info("Didn't get ICEInit Candidates from coordinator")
	}

	logger.Logger.Info("Finished getting ICEInit Candidates from coordinator")
	return nil, ErrCantGetICECandidates
}

func (c httpPeerConnector) stop() {

}

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
