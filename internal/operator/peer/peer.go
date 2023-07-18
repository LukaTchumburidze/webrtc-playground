package peer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/pion/randutil"
	"github.com/pion/webrtc/v3"
	"net/http"
	"os"
	"time"
	"webrtc-playground/internal/operator/coordinator"
)

const N_OF_MESSAGES = 5
const BUSY_TIMEOUT = 20 * time.Second
const DELAY_AFTER_OFFER = 10 * time.Second
const DELAY_BEFORE_ANSWER = 10 * time.Second
const TYPE_APP_JSON = "application/json; charset=utf-8"

func randSeq(n int) string {
	val, err := randutil.GenerateCryptoRandomString(n, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
	if err != nil {
		panic(err)
	}

	return val
}

// TODO: Leftovers from pion example, might change later
func signalCandidate(addr string, c *webrtc.ICECandidate) error {
	payload := []byte(c.ToJSON().Candidate)
	resp, err := http.Post(fmt.Sprintf("http://%s/candidate", addr), // nolint:noctx
		"application/json; charset=utf-8", bytes.NewReader(payload))
	if err != nil {
		return err
	}

	return resp.Body.Close()
}

type Peer struct {
	CoordinatorAddress string
	CoordinatorPort    int
	PeerConnection     *webrtc.PeerConnection
	waitChannel        chan bool
	sentMsgCnt         int
	receivedMsgCnt     int
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
	if err == nil {
		return
	}

	// Set the handler for Peer connection state
	// This will notify you when the peer has connected/disconnected
	peerConnection.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		fmt.Printf("Peer Connection State has changed: %s\n", s.String())

		if s == webrtc.PeerConnectionStateFailed {
			// Await until PeerConnection has had no network activity for 30 seconds or another failure. It may be reconnected using an ICE Restart.
			// Use webrtc.PeerConnectionStateDisconnected if you are interested in detecting faster timeout.
			// Note that the PeerConnection may come back from PeerConnectionStateDisconnected.
			fmt.Println("Peer Connection has gone to failed exiting")
			os.Exit(0)
		}
	})

	peerConnection.OnDataChannel(func(channel *webrtc.DataChannel) {
		peer.onDataChannel(channel)
	})

	return &Peer{
		CoordinatorAddress: coordinatorAddress,
		CoordinatorPort:    coordinatorPort,
		PeerConnection:     peerConnection,
		waitChannel:        make(chan bool),
	}, nil
}

func (receiver *Peer) Await() {
	fmt.Printf("Waiting to stop\n")
	select {
	case val := <-receiver.waitChannel:
		fmt.Printf("Peer was successful: %v\n", val)
	}
}

func (receiver *Peer) Stop(val bool) {
	receiver.waitChannel <- val
}

func (receiver *Peer) onDataChannel(d *webrtc.DataChannel) {
	fmt.Printf("New DataChannel %s %d\n", d.Label(), d.ID())

	// Register channel opening handling
	d.OnOpen(func() {
		fmt.Printf("Data channel '%s'-'%d' open. Random messages will now be sent to any connected DataChannels every 5 seconds\n", d.Label(), d.ID())

		for range time.NewTicker(5 * time.Second).C {
			if receiver.sentMsgCnt == N_OF_MESSAGES {
				fmt.Printf("Total of %v messages were sent, send finished\n", N_OF_MESSAGES)
				break
			}
			message := randSeq(15)
			fmt.Printf("Sending '%s'\n", message)

			// Send the message as text
			sendTextErr := d.SendText(message)
			if sendTextErr != nil {
				panic(sendTextErr)
			}
			receiver.sentMsgCnt++
		}
	})

	// Register text message handling
	d.OnMessage(func(msg webrtc.DataChannelMessage) {
		if receiver.receivedMsgCnt == N_OF_MESSAGES {
			fmt.Printf("Total of %v messages were sent, node should stop\n", N_OF_MESSAGES)
			receiver.Stop(true)
		}
		fmt.Printf("Message from DataChannel '%s': '%s'\n", d.Label(), string(msg.Data))
		receiver.receivedMsgCnt++
	})
}

func (receiver *Peer) InitConnection() error {
	//Init connection with coordinator

	status := coordinator.StatusStruct{Status: coordinator.STATUS_BUSY}

	// TODO: use coordinator to get role (offer/answer)
	for status.Status == coordinator.STATUS_BUSY {
		resp, err := http.Post(fmt.Sprint("%s:%d%s", receiver.CoordinatorAddress, receiver.CoordinatorPort, coordinator.HTTP_REGISTER_PATH),
			TYPE_APP_JSON,
			bytes.NewReader([]byte{}))
		if err != nil {
			fmt.Fprint(os.Stderr, err)
			return err
		} else if err := resp.Body.Close(); err != nil {
			fmt.Fprint(os.Stderr, err)
			return err
		}
		err = json.NewDecoder(resp.Body).Decode(&status)
		if err != nil {
			fmt.Fprint(os.Stderr, err)
			return err
		}

		fmt.Printf("Sleeping for %d nanos", status.Status, BUSY_TIMEOUT)
	}

	offer, err := receiver.PeerConnection.CreateOffer(nil)
	if err != nil {
		fmt.Fprint(os.Stderr, err)
		return err
	}
	payload, err := json.Marshal(offer)
	if err != nil {
		fmt.Fprint(os.Stderr, err)
		return err
	}

	switch status.Status {
	case coordinator.ROLE_OFFER:
		// SEND offer role
		fmt.Printf("Sending Offer SDP\n")
		_, err := http.Post(fmt.Sprint("%s:%d%s", receiver.CoordinatorAddress, receiver.CoordinatorPort, coordinator.HTTP_SDP_OFFER_PATH),
			TYPE_APP_JSON,
			bytes.NewReader(payload))
		if err != nil {
			fmt.Fprint(os.Stderr, err)
			return err
		}

		if err = receiver.PeerConnection.SetLocalDescription(offer); err != nil {
			fmt.Fprint(os.Stderr, err)
			return err
		}

		fmt.Printf("Sleping after offer\n")
		time.Sleep(DELAY_AFTER_OFFER)

		// RECEIVE answer connection
		fmt.Printf("Getting answer SDP\n")
		resp, err := http.Get(fmt.Sprint("%s:%d%s", receiver.CoordinatorAddress, receiver.CoordinatorPort, coordinator.HTTP_SDP_ANSWER_PATH))
		if err != nil {
			fmt.Fprint(os.Stderr, err)
			return err
		} else if err := resp.Body.Close(); err != nil {
			fmt.Fprint(os.Stderr, err)
			return err
		}
		var answerSdp webrtc.SessionDescription
		err = json.NewDecoder(resp.Body).Decode(&answerSdp)
		if err := resp.Body.Close(); err != nil {
			fmt.Fprint(os.Stderr, err)
			return err
		}
		err = receiver.PeerConnection.SetRemoteDescription(answerSdp)
		if err != nil {
			fmt.Fprint(os.Stderr, err)
			return err
		}
	case coordinator.ROLE_ANSWER:
		//fmt.Printf("Sleping before answer\n")
		//time.Sleep(DELAY_BEFORE_ANSWER)
		// Receive offer
		fmt.Printf("Getting offer SDP\n")
		resp, err := http.Get(fmt.Sprint("%s:%d%s", receiver.CoordinatorAddress, receiver.CoordinatorPort, coordinator.HTTP_SDP_OFFER_PATH))
		if err != nil {
			fmt.Fprint(os.Stderr, err)
			return err
		} else if err := resp.Body.Close(); err != nil {
			fmt.Fprint(os.Stderr, err)
			return err
		}
		var offerSdp webrtc.SessionDescription
		err = json.NewDecoder(resp.Body).Decode(&offerSdp)

		err = receiver.PeerConnection.SetRemoteDescription(offerSdp)
		if err != nil {
			fmt.Fprint(os.Stderr, err)
			return err
		}

		answerSdp, err := receiver.PeerConnection.CreateAnswer(nil)
		if err != nil {
			fmt.Fprint(os.Stderr, err)
			return err
		}
		err = receiver.PeerConnection.SetLocalDescription(answerSdp)
		if err != nil {
			fmt.Fprint(os.Stderr, err)
			return err
		}

		fmt.Printf("Sending answer SDP\n")
		_, err = http.Post(fmt.Sprint("%s:%d%s", receiver.CoordinatorAddress, receiver.CoordinatorPort, coordinator.HTTP_SDP_ANSWER_PATH),
			TYPE_APP_JSON,
			bytes.NewReader(payload))
		if err != nil {
			fmt.Fprint(os.Stderr, err)
			return err
		}
	}

	_, err = http.Post(fmt.Sprint("%s:%d%s", receiver.CoordinatorAddress, receiver.CoordinatorPort, coordinator.HTTP_SDP_ANSWER_PATH),
		TYPE_APP_JSON,
		bytes.NewReader([]byte{}))
	if err != nil {
		fmt.Fprint(os.Stderr, err)
		return err
	}

	fmt.Printf("Peer with role %v done", status.Status)
	return nil
}

func (receiver *Peer) SendData() {
	// Create a datachannel with label	 'data'
	dataChannel, err := receiver.PeerConnection.CreateDataChannel("data", nil)
	if err != nil {
		panic(err)
	}

	dataChannel.OnOpen(func() {
		fmt.Printf("Data channel '%s'-'%d' open. Random messages will now be sent to any connected DataChannels every 5 seconds\n", dataChannel.Label(), dataChannel.ID())

		for range time.NewTicker(5 * time.Second).C {
			message := randSeq(15)
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
}
