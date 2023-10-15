package peer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/pion/webrtc/v3"
	"net/http"
	"os"
	"sync"
	"time"
	"webrtc-playground/internal/model"
	"webrtc-playground/internal/operator/coordinator"
)

const N_OF_MESSAGES = 5
const BUSY_TIMEOUT = 20 * time.Second
const DELAY_AFTER_OFFER = 5 * time.Second
const DELAY_BEFORE_ANSWER = 5 * time.Second
const TYPE_APP_JSON = "application/json; charset=utf-8"
const URL_FORMAT = "http://%s:%d%s"

const (
	ICE_GET_DELAY  = 5 * time.Second
	ICE_SEND_DELAY = 5 * time.Second
	ICE_GET_TRIES  = 10
)

func (receiver *Peer) signalCandidates() error {
	if receiver.id == "" {
		panic("id is not set, can't send candidate")
	}

	fmt.Printf("Sending %v candidates to coordinator\n", len(receiver.pendingCandidates))

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

	fmt.Printf("Started getting ICEInit Candidates from coordinator at %v:%v\n", receiver.CoordinatorAddress, receiver.CoordinatorPort)
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

		fmt.Printf("Received %v candidates from coordinator\n", len(iceCandidates))
	}
	fmt.Printf("Finished getting ICEInit candidates\n")

	return nil
}

type Peer struct {
	CoordinatorAddress string
	CoordinatorPort    int
	PeerConnection     *webrtc.PeerConnection
	waitChannel        chan error
	pendingCandidates  []*webrtc.ICECandidateInit
	candidatesMux      sync.Mutex
	sentMsgCnt         int
	receivedMsgCnt     int
	id                 string
}

func New(coordinatorAddress string, coordinatorPort int) (peer *Peer, err error) {
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
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
	}

	// Set the handler for Peer connection state
	// This will notify you when the peer has connected/disconnected
	peerConnection.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		fmt.Printf("Peer Connection State has changed: %s\n", s.String())

		if s == webrtc.PeerConnectionStateFailed {
			// Await until PeerConnection has had no network activity for 30 seconds or another failure. It may be reconnected using an ICEInit Restart.
			// Use webrtc.PeerConnectionStateDisconnected if you are interested in detecting faster timeout.
			// Note that the PeerConnection may come back from PeerConnectionStateDisconnected.

			peer.stop(fmt.Errorf("Peer Connection has gone to failed exiting\n"))
		}
	})

	return
}

func (receiver *Peer) Await() error {
	fmt.Printf("Waiting to stop\n")
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
	fmt.Printf("Sending Offer SDP\n")

	offer, err := receiver.PeerConnection.CreateOffer(nil)
	if err != nil {
		return err
	}

	payload, err := json.Marshal(offer)
	if err != nil {
		return err
	}

	curPath := fmt.Sprintf(URL_FORMAT, receiver.CoordinatorAddress, receiver.CoordinatorPort, coordinator.HTTP_SDP_OFFER_PATH)
	_, err = http.Post(curPath,
		TYPE_APP_JSON,
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
	fmt.Printf("Getting answer SDP\n")

	curPath := fmt.Sprintf(URL_FORMAT, receiver.CoordinatorAddress, receiver.CoordinatorPort, coordinator.HTTP_SDP_ANSWER_PATH)
	resp, err := http.Get(curPath)
	if err != nil {
		fmt.Fprint(os.Stderr, err)
		return "", err
	}
	var answerPayload model.SDPPayload
	err = json.NewDecoder(resp.Body).Decode(&answerPayload)
	if err := resp.Body.Close(); err != nil {
		fmt.Fprint(os.Stderr, err)
		return "", err
	}
	err = receiver.PeerConnection.SetRemoteDescription(*answerPayload.Sdp)
	if err != nil {
		fmt.Fprint(os.Stderr, err)
		return "", err
	}

	return answerPayload.ID, nil
}

func (receiver *Peer) setupDataChannel() error {
	dataChannel, err := receiver.PeerConnection.CreateDataChannel(coordinator.RandSeq(5), nil)
	if err != nil {
		return err
	}

	dataChannel.OnOpen(func() {
		fmt.Printf("Data channel '%s'-'%d' open. Random messages will now be sent to any connected DataChannels every 5 seconds\n", dataChannel.Label(), dataChannel.ID())

		for range time.NewTicker(5 * time.Second).C {
			message := coordinator.RandSeq(15)
			fmt.Printf("Sending '%s'\n", message)

			// Send the message as text
			sendTextErr := dataChannel.SendText(message)
			if sendTextErr != nil {
				panic(sendTextErr)
			}
		}
	})

	// Register text message handling
	dataChannel.OnMessage(func(msg webrtc.DataChannelMessage) {
		fmt.Printf("Message from DataChannel '%s': '%s'\n", dataChannel.Label(), string(msg.Data))
	})

	return nil
}

func (receiver *Peer) resolveOffer() error {
	if err := receiver.setupDataChannel(); err != nil {
		return nil
	}

	if err := receiver.sendOfferSDP(); err != nil {
		return err
	}

	fmt.Printf("Sleeping after offer\n")
	time.Sleep(DELAY_AFTER_OFFER)

	id, err := receiver.getAnswerSDPID()
	if err != nil {
		return err
	}

	receiver.id = id

	return nil
}

func (receiver *Peer) getOfferSDPID() (string, error) {
	curPath := fmt.Sprintf(URL_FORMAT, receiver.CoordinatorAddress, receiver.CoordinatorPort, coordinator.HTTP_SDP_OFFER_PATH)

	resp, err := http.Get(curPath)
	if err != nil {
		return "", err
	}

	var offerPayload model.SDPPayload
	err = json.NewDecoder(resp.Body).Decode(&offerPayload)
	if err := resp.Body.Close(); err != nil {
		return "", err
	}
	err = receiver.PeerConnection.SetRemoteDescription(*offerPayload.Sdp)
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

	fmt.Printf("Sending answer SDP\n")
	curPath := fmt.Sprintf(URL_FORMAT, receiver.CoordinatorAddress, receiver.CoordinatorPort, coordinator.HTTP_SDP_ANSWER_PATH)
	_, err = http.Post(curPath,
		TYPE_APP_JSON,
		bytes.NewReader(payload))
	if err != nil {
		fmt.Fprint(os.Stderr, err)
		return err
	}

	return nil
}

func (receiver *Peer) resolveAnswer() error {
	fmt.Printf("Getting offer SDP\n")

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
	switch status {
	case coordinator.ROLE_OFFER:
		err := receiver.resolveOffer()
		if err != nil {
			return err
		}

		curPath := fmt.Sprintf(URL_FORMAT, receiver.CoordinatorAddress, receiver.CoordinatorPort, coordinator.HTTP_REGISTER_DONE_PATH)
		_, err = http.Post(curPath,
			TYPE_APP_JSON,
			bytes.NewReader([]byte{}))
		if err != nil {
			return err
		}

		fmt.Printf("Registration done")
	case coordinator.ROLE_ANSWER:
		receiver.PeerConnection.OnDataChannel(func(channel *webrtc.DataChannel) {
			channel.OnMessage(func(msg webrtc.DataChannelMessage) {
				fmt.Printf("Message from DataChannel '%s': '%s'\n", channel.Label(), string(msg.Data))

				channel.SendText("echo " + string(msg.Data))
			})
		})

		err := receiver.resolveAnswer()
		if err != nil {
			return err
		}
	}

	time.Sleep(ICE_SEND_DELAY)
	err := receiver.signalCandidates()
	if err != nil {
		return nil
	}
	time.Sleep(ICE_GET_DELAY)
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

		fmt.Printf("Adding candidate %v\n", c.String())

		receiver.pendingCandidates = append(receiver.pendingCandidates, &webrtc.ICECandidateInit{Candidate: c.ToJSON().Candidate})
	})

	status := coordinator.STATUS_BUSY

	for status == coordinator.STATUS_BUSY {
		curPath := fmt.Sprintf(URL_FORMAT, receiver.CoordinatorAddress, receiver.CoordinatorPort, coordinator.HTTP_REGISTER_PATH)
		resp, err := http.Post(curPath,
			TYPE_APP_JSON,
			bytes.NewReader([]byte{}))
		if err != nil {
			fmt.Fprint(os.Stderr, err)
			return err
		}
		err = json.NewDecoder(resp.Body).Decode(&status)
		if err != nil {
			fmt.Fprint(os.Stderr, err)
			return err
		}

		fmt.Printf("RoleStatus is %v, Sleeping for %v\n", status, BUSY_TIMEOUT)
	}

	err := receiver.resolveStatus(status)

	if err != nil {
		fmt.Fprint(os.Stderr, err)
		return err
	}

	fmt.Printf("Peer with role %v done\n", status)
	return nil
}
