package peer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/pion/randutil"
	"github.com/pion/webrtc/v3"
	"net/http"
	"time"
)

const (
	GOOGLE_STUN_ADDRESS = "stun:stun.l.google.com:19302"
	N_OF_MESSAGES       = 5
)

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
	waitChannel        chan error
	sentMsgCnt         int
	receivedMsgCnt     int
}

func New(coordinatorAddress string, coordinatorPort int) (peer *Peer, err error) {
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{GOOGLE_STUN_ADDRESS},
			},
		},
	}
	peerConnection, err := webrtc.NewPeerConnection(config)
	if err == nil {
		return
	}

	peer = &Peer{
		CoordinatorAddress: coordinatorAddress,
		CoordinatorPort:    coordinatorPort,
		PeerConnection:     peerConnection,
		waitChannel:        make(chan error),
	}

	// Set the handler for Peer connection state
	// This will notify you when the peer has connected/disconnected
	peerConnection.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		fmt.Printf("Peer Connection State has changed: %s\n", s.String())

		if s == webrtc.PeerConnectionStateFailed {
			// Await until PeerConnection has had no network activity for 30 seconds or another failure. It may be reconnected using an ICE Restart.
			// Use webrtc.PeerConnectionStateDisconnected if you are interested in detecting faster timeout.
			// Note that the PeerConnection may come back from PeerConnectionStateDisconnected.

			peer.stop(fmt.Errorf("Peer Connection has gone to failed exiting\""))
		}
	})

	peerConnection.OnDataChannel(func(channel *webrtc.DataChannel) {
		peer.onDataChannel(channel)
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
			receiver.stop(nil)
		}
		fmt.Printf("Message from DataChannel '%s': '%s'\n", d.Label(), string(msg.Data))
		receiver.receivedMsgCnt++
	})
}

func (receiver *Peer) InitConnection() error {
	//Init connection with coordinator

	// Create an offer to send to the other process
	offer, err := receiver.PeerConnection.CreateOffer(nil)
	if err != nil {
		return err
	}
	if err = receiver.PeerConnection.SetLocalDescription(offer); err != nil {
		return err
	}

	// Send our offer to the HTTP server listening in the other process
	payload, err := json.Marshal(offer)
	if err != nil {
		panic(err)
	}
	resp, err := http.Post(fmt.Sprintf("http://%s:%d/sdp", receiver.CoordinatorAddress, receiver.CoordinatorPort), "application/json; charset=utf-8", bytes.NewReader(payload)) // nolint:noctx
	if err != nil {
		panic(err)
	} else if err := resp.Body.Close(); err != nil {
		panic(err)
	}

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
