package coordinator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/pion/webrtc/v3"
	"net/http"
	"os"
	"sync"
	"time"
)

const (
	STATUS_BUSY = Status("BUSY")
	ROLE_OFFER  = Status("OFFER")
	ROLE_ANSWER = Status("ANSWER")

	HTTP_REGISTER_PATH      = "/register"
	HTTP_SDP_OFFER_PATH     = "/sdp/offer"
	HTTP_SDP_ANSWER_PATH    = "/sdp/answer"
	HTTP_REGISTER_DONE_PATH = "/register/done"

	MATCHING_TIMEOUT = 60 * time.Second
)

type Status string

// TODO: Right now ICE candidates optimization is fully removed, might add later

type Coordinator struct {
	port         int
	offers       []*webrtc.SessionDescription
	answers      []*webrtc.SessionDescription
	candidates   [][]webrtc.ICECandidate // TODO: Maybe use it, maybe not
	isBusy       bool
	doneCnt      int
	peersMux     sync.Mutex
	awaitChannel chan bool
}

type pairedPeer struct {
	first  *webrtc.PeerConnection
	second *webrtc.PeerConnection
}

func New(port int) (*Coordinator, error) {
	return &Coordinator{
		port:         port,
		offers:       make([]*webrtc.SessionDescription, 0),
		answers:      make([]*webrtc.SessionDescription, 0),
		awaitChannel: make(chan bool),
		peersMux:     sync.Mutex{},
	}, nil
}

func (receiver *Coordinator) handleRegister() {
	http.HandleFunc(HTTP_REGISTER_PATH, func(w http.ResponseWriter, r *http.Request) {
		receiver.peersMux.Lock()
		defer receiver.peersMux.Unlock()

		if receiver.isBusy {
			buff := bytes.Buffer{}
			err := json.NewEncoder(&buff).Encode(Status(STATUS_BUSY))
			if err != nil {
				fmt.Fprint(os.Stderr, err)
				return
			}
			_, err = w.Write(buff.Bytes())
			if err != nil {
				fmt.Fprint(os.Stderr, err)
				return
			}

			return
		}

		if len(receiver.offers) == len(receiver.answers) {
			buff := bytes.Buffer{}
			err := json.NewEncoder(&buff).Encode(Status(ROLE_OFFER))
			if err != nil {
				fmt.Fprint(os.Stderr, err)
				return
			}
			_, err = w.Write(buff.Bytes())
			if err != nil {
				fmt.Fprint(os.Stderr, err)
				return
			}

			receiver.offers = append(receiver.offers, nil)
		} else {
			buff := bytes.Buffer{}
			err := json.NewEncoder(&buff).Encode(Status(ROLE_ANSWER))
			if err != nil {
				fmt.Fprint(os.Stderr, err)
				return
			}
			_, err = w.Write(buff.Bytes())
			if err != nil {
				fmt.Fprint(os.Stderr, err)
				return
			}

			receiver.answers = append(receiver.answers, nil)
			receiver.isBusy = true

			go receiver.resetBusyState()
		}
	})

	// Should be called by offer peer once it retrieves answer's SDP
	http.HandleFunc(HTTP_REGISTER_DONE_PATH, func(w http.ResponseWriter, r *http.Request) {
		receiver.peersMux.Lock()
		defer receiver.peersMux.Unlock()

		fmt.Printf("Done received from %v", r.RemoteAddr)
		receiver.doneCnt++
		if receiver.doneCnt == 2 {
			receiver.doneCnt = 0
			receiver.isBusy = false
		}
	})
}

func (receiver *Coordinator) resetBusyState() {
	receiver.peersMux.Lock()
	defer receiver.peersMux.Unlock()

	oldLen := len(receiver.offers)
	time.Sleep(MATCHING_TIMEOUT)

	if len(receiver.offers) == oldLen &&
		(receiver.offers[oldLen-1] == nil ||
			receiver.answers[oldLen-1] == nil) {
		fmt.Printf("After timeout both peers wasn't registered, removing last 2 records from offers/answers")

		receiver.offers = receiver.offers[:oldLen-1]
		receiver.answers = receiver.answers[:oldLen-1]
		receiver.isBusy = false
	}
}

func resetBusyState() {

}

func (receiver *Coordinator) handleSdp() {
	http.HandleFunc(HTTP_SDP_OFFER_PATH, func(w http.ResponseWriter, r *http.Request) {
		receiver.peersMux.Lock()
		defer receiver.peersMux.Unlock()

		switch r.Method {
		case http.MethodPost:
			// Offer giving us SDP
			sdp := webrtc.SessionDescription{}
			if err := json.NewDecoder(r.Body).Decode(&sdp); err != nil {
				fmt.Fprint(os.Stderr, err)
				return
			}
			fmt.Printf("Received SDP from offer peer %v\n", sdp)
			receiver.offers[len(receiver.offers)-1] = &sdp
		case http.MethodGet:
			// Answer asking for offer's SDP
			sdp := receiver.offers[len(receiver.offers)-1]
			if err := json.NewEncoder(w).Encode(sdp); err != nil {
				fmt.Fprint(os.Stderr, err)
				return
			}
			fmt.Printf("Sending SDP to answer peer %v\n", sdp)
		}
	})

	http.HandleFunc(HTTP_SDP_ANSWER_PATH, func(w http.ResponseWriter, r *http.Request) {
		receiver.peersMux.Lock()
		defer receiver.peersMux.Unlock()

		switch r.Method {
		case http.MethodPost:
			// Answer giving us SDP
			sdp := webrtc.SessionDescription{}
			if err := json.NewDecoder(r.Body).Decode(&sdp); err != nil {
				fmt.Fprint(os.Stderr, err)
				return
			}
			fmt.Printf("Received SDP from answer peer %v\n", sdp)
			receiver.answers[len(receiver.answers)-1] = &sdp
		case http.MethodGet:
			// Offer asking for Answer's SDP
			sdp := receiver.answers[len(receiver.answers)-1]
			if err := json.NewEncoder(w).Encode(receiver.answers[len(receiver.answers)-1]); err != nil {
				fmt.Fprint(os.Stderr, err)
				return
			}
			fmt.Printf("Sending SDP to offer peer %v\n", sdp)
		}
	})
}

func (receiver *Coordinator) Listen() {
	select {
	case val := <-receiver.awaitChannel:
		fmt.Printf("Coordinator was successful: %v\n", val)
	}
}

func (receiver *Coordinator) StopListening(val bool) {
	receiver.awaitChannel <- val
}
