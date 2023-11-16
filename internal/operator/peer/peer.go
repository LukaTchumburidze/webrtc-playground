package peer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/pion/webrtc/v3"
	"net/http"
	"os"
	"sync"
	"time"
	"webrtc-playground/internal/model"
	"webrtc-playground/internal/operator/coordinator"
	"webrtc-playground/internal/worker"
)

const (
	BUSY_TIMEOUT        = 20 * time.Second
	DELAY_AFTER_OFFER   = 5 * time.Second
	TYPE_APP_JSON       = "application/json; charset=utf-8"
	URL_FORMAT          = "http://%s:%d%s"
	GOOGLE_STUN_ADDRESS = "stun:stun.l.google.com:19302"
)
const (
	ICE_GET_DELAY  = 5 * time.Second
	ICE_SEND_DELAY = 5 * time.Second
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

	worker *worker.Worker
}

func New(coordinatorAddress string, coordinatorPort int, worker *worker.Worker) (peer *Peer, err error) {
	if worker == nil {
		return nil, errors.New("nil worker has been passed to peer")
	}

	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{GOOGLE_STUN_ADDRESS},
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
		fmt.Printf("Peer Connection State has changed: %s\n", s.String())

		if s == webrtc.PeerConnectionStateFailed {
			// Await until PeerConnection has had no network activity for 30 seconds or another failure. It may be reconnected using an ICEInit Restart.
			// Use webrtc.PeerConnectionStateDisconnected if you are interested in detecting faster timeout.
			// Note that the PeerConnection may come back from PeerConnectionStateDisconnected.

			peer.stop(fmt.Errorf("Peer Connection has gone to failed exiting\n"))
		}

		//TODO: Currently connection state disconnected is being regarded as valid exit, don't think this is correct
		if s == webrtc.PeerConnectionStateClosed || s == webrtc.PeerConnectionStateDisconnected {
			fmt.Printf("Valid terminal state change, exitting \n")
			peer.Stop()
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

	fmt.Printf("Created DataChannel %v\n", dataChannel.Label())

	dataChannel.OnOpen(func() {
		var err error
		for err != worker.ErrFinish {
			b, err := (*receiver.worker).ProducePayload()
			if err != nil {
				if err == worker.ErrFinish {
					break
				} else {
					panic(err)
				}
			}
			err = dataChannel.Send(b)
		}
		fmt.Printf("Worker finished producing payload, closing DataChannel\n")
		dataChannel.Close()

		err = receiver.PeerConnection.Close()
		if err != nil {
			fmt.Errorf("%v\n", err)
		}
	})

	// Current version basically is just one DataChannel, if it gets closed, peers should finish work
	dataChannel.OnClose(func() {
		fmt.Printf("DataChannel was closed, closing peer connection")
		receiver.PeerConnection.Close()
	})

	receiver.PeerConnection.OnDataChannel(func(channel *webrtc.DataChannel) {
		fmt.Printf("OnDataChannel %v\n", channel.Label())
		// Register text message handling
		channel.OnMessage(func(msg webrtc.DataChannelMessage) {
			b := msg.Data
			err = (*receiver.worker).ConsumePayload(b)
			if err != nil {
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
	if err := receiver.setupDataChannel(); err != nil {
		return nil
	}

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

		fmt.Printf("Registration done\n")
	case coordinator.ROLE_ANSWER:
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
