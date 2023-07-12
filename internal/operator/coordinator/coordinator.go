package coordinator

import (
	"encoding/json"
	"fmt"
	"github.com/pion/webrtc/v3"
	"net/http"
	"os"
	"sync"
	"time"
)

const (
	STATUS_BUSY = "BUSY"
	ROLE_OFFER  = "OFFER"
	ROLE_ANSWER = "ANSWER"

	HTTP_GET  = "GET"
	HTTP_POST = "POST"
)

// TODO: Right now ICE candidates optimization is fully removed, might add later

type Coordinator struct {
	port         int
	offers       []*webrtc.SessionDescription
	answers      []*webrtc.SessionDescription
	candidates   [][]webrtc.ICECandidate // TODO: Maybe use it, maybe not
	isBusy       bool
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
	http.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		receiver.peersMux.Lock()
		defer receiver.peersMux.Unlock()

		if receiver.isBusy {
			_, err := w.Write([]byte(STATUS_BUSY))
			if err != nil {
				fmt.Fprint(os.Stderr, err)
			}

			return
		}

		if len(receiver.offers) == len(receiver.answers) {
			receiver.offers = append(receiver.offers, nil)
			_, err := w.Write([]byte(ROLE_OFFER))
			if err != nil {
				if err != nil {
					fmt.Fprint(os.Stderr, err)
				}
			}
		} else {
			receiver.answers = append(receiver.answers, nil)
			_, err := w.Write([]byte(ROLE_ANSWER))
			if err != nil {
				if err != nil {
					fmt.Fprint(os.Stderr, err)
				}
			}

			receiver.isBusy = true

			go func() {
				receiver.peersMux.Lock()
				defer receiver.peersMux.Unlock()

				oldLen := len(receiver.offers)
				time.Sleep(30 * time.Second)

				if len(receiver.offers) == oldLen &&
					(receiver.offers[oldLen-1] == nil ||
						receiver.answers[oldLen-1] == nil) {
					fmt.Printf("After timeout both peers wasn't registered, removing last 2 records from offers/answers")

					receiver.offers = receiver.offers[:oldLen-1]
					receiver.answers = receiver.answers[:oldLen-1]
					receiver.isBusy = false
				}
			}()
		}
	})

	// Should be called by offer peer once it retrieves answer's SDP
	http.HandleFunc("/register/done", func(w http.ResponseWriter, r *http.Request) {
		receiver.isBusy = false
	})
}

func (receiver *Coordinator) handleSdp() {
	http.HandleFunc("/sdp/offer", func(w http.ResponseWriter, r *http.Request) {
		receiver.peersMux.Lock()
		defer receiver.peersMux.Unlock()

		switch r.Method {
		case HTTP_POST:
			// Offer giving us SDP
			sdp := webrtc.SessionDescription{}
			if err := json.NewDecoder(r.Body).Decode(&sdp); err != nil {
				fmt.Fprint(os.Stderr, err)
			}
			fmt.Printf("Received SDP from offer peer %v\n", sdp)
			receiver.offers[len(receiver.offers)-1] = &sdp
		case HTTP_GET:
			// Answer asking for offer's SDP
			sdp := receiver.offers[len(receiver.offers)-1]
			if err := json.NewEncoder(w).Encode(sdp); err != nil {
				fmt.Fprint(os.Stderr, err)
			}
			fmt.Printf("Sending SDP to answer peer %v\n", sdp)
		}
	})

	http.HandleFunc("/sdp/answer", func(w http.ResponseWriter, r *http.Request) {
		receiver.peersMux.Lock()
		defer receiver.peersMux.Unlock()

		switch r.Method {
		case HTTP_POST:
			// Answer giving us SDP
			sdp := webrtc.SessionDescription{}
			if err := json.NewDecoder(r.Body).Decode(&sdp); err != nil {
				fmt.Fprint(os.Stderr, err)
			}
			fmt.Printf("Received SDP from answer peer %v\n", sdp)
			receiver.answers[len(receiver.answers)-1] = &sdp
		case HTTP_GET:
			// Offer asking for Answer's SDP
			sdp := receiver.answers[len(receiver.answers)-1]
			if err := json.NewEncoder(w).Encode(receiver.answers[len(receiver.answers)-1]); err != nil {
				fmt.Fprint(os.Stderr, err)
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
