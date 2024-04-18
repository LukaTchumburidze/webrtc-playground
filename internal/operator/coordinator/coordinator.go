package coordinator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/pion/randutil"
	"github.com/pion/webrtc/v3"
	"io"
	"net/http"
	"os"
	"sync"
	"time"
	"webrtc-playground/internal/model"
)

const (
	StatusBusy = model.RoleStatus("BUSY")
	RoleOffer  = model.RoleStatus("OFFER")
	RoleAnswer = model.RoleStatus("ANSWER")
)
const (
	HTTPICEPath          = "/ice"
	HttpRegisterPath     = "/register"
	HttpSDPPath          = "/sdp/offer"
	HttpSDPAnswerPath    = "/sdp/answer"
	HttpRegisterDonePath = "/register/done"

	MatchingTimeout = 60 * time.Second
)

const IDLength = 5

func RandSeq(n int) string {
	val, err := randutil.GenerateCryptoRandomString(n, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
	if err != nil {
		panic(err)
	}

	return val
}

type Coordinator struct {
	port              int
	offers            []*model.SDPPayload
	answers           []*model.SDPPayload
	isBusy            bool
	doneCnt           int
	peersMux          sync.Mutex
	awaitChannel      chan bool
	connectedPeers    map[string]string
	peerICECandidates map[string][]webrtc.ICECandidateInit
}

func New(port uint16) (*Coordinator, error) {
	return &Coordinator{
		port:              int(port),
		offers:            make([]*model.SDPPayload, 0),
		answers:           make([]*model.SDPPayload, 0),
		awaitChannel:      make(chan bool),
		peersMux:          sync.Mutex{},
		connectedPeers:    make(map[string]string),
		peerICECandidates: make(map[string][]webrtc.ICECandidateInit),
	}, nil
}

func (receiver *Coordinator) handleRegister() {
	http.HandleFunc(HttpRegisterPath, func(w http.ResponseWriter, r *http.Request) {
		receiver.peersMux.Lock()
		defer receiver.peersMux.Unlock()

		fmt.Printf("Registration received from %v\n", r.RemoteAddr)

		if receiver.isBusy {
			buff := bytes.Buffer{}
			err := json.NewEncoder(&buff).Encode(model.RoleStatus(StatusBusy))
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
			err := json.NewEncoder(&buff).Encode(model.RoleStatus(RoleOffer))
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
			err := json.NewEncoder(&buff).Encode(model.RoleStatus(RoleAnswer))
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
	http.HandleFunc(HttpRegisterDonePath, func(w http.ResponseWriter, r *http.Request) {
		receiver.peersMux.Lock()
		defer receiver.peersMux.Unlock()

		offerSDP := receiver.offers[len(receiver.offers)-1]
		answerSDP := receiver.answers[len(receiver.answers)-1]

		receiver.connectedPeers[offerSDP.ID] = answerSDP.ID
		receiver.connectedPeers[answerSDP.ID] = offerSDP.ID

		fmt.Printf("Registration done received from %v\n", r.RemoteAddr)
		receiver.doneCnt++
		if receiver.doneCnt == 2 {
			receiver.doneCnt = 0
			receiver.isBusy = false
		}
	})
}

func (receiver *Coordinator) resetBusyState() {
	oldLen := len(receiver.offers)
	time.Sleep(MatchingTimeout)

	if len(receiver.offers) == oldLen &&
		(receiver.offers[oldLen-1] == nil ||
			receiver.answers[oldLen-1] == nil) {
		fmt.Printf("After timeout both peers weren't registered, removing last 2 records from offers/answers\n")

		receiver.peersMux.Lock()

		receiver.offers = receiver.offers[:oldLen-1]
		receiver.answers = receiver.answers[:oldLen-1]
		receiver.isBusy = false

		receiver.peersMux.Unlock()
	}
}

func (receiver *Coordinator) handleSdp() {
	http.HandleFunc(HttpSDPPath, func(w http.ResponseWriter, r *http.Request) {
		receiver.peersMux.Lock()
		defer receiver.peersMux.Unlock()

		fmt.Printf("Received %v on SDP_OFFER endpoint for %v\n", r.Method, r.RemoteAddr)

		switch r.Method {
		case http.MethodPost:
			// Offer giving us SDP
			sdp := webrtc.SessionDescription{}
			if err := json.NewDecoder(r.Body).Decode(&sdp); err != nil {
				fmt.Fprint(os.Stderr, err)
				return
			}
			sdpPayload := &model.SDPPayload{
				ID:  RandSeq(IDLength),
				SDP: &sdp,
			}
			fmt.Printf("Received SDP from offer peer %v\n", sdpPayload.ID)
			receiver.offers[len(receiver.offers)-1] = sdpPayload

		case http.MethodGet:
			// Answer asking for offer's SDP
			if len(receiver.offers) == 0 {
				fmt.Fprint(os.Stderr, "So there's no offer SDP to send\n")
				return
			}
			sdpPayload := receiver.offers[len(receiver.offers)-1]
			if err := json.NewEncoder(w).Encode(sdpPayload); err != nil {
				fmt.Fprint(os.Stderr, err)
				return
			}
			fmt.Printf("Sending SDPPayload to answer peer %v\n", sdpPayload.ID)
		}
	})

	http.HandleFunc(HttpSDPAnswerPath, func(w http.ResponseWriter, r *http.Request) {
		receiver.peersMux.Lock()
		defer receiver.peersMux.Unlock()

		fmt.Printf("Received %v on SDP_ANSWER endpoint for %v\n", r.Method, r.RemoteAddr)

		switch r.Method {
		case http.MethodPost:
			// Answer giving us SDP
			sdp := webrtc.SessionDescription{}
			if err := json.NewDecoder(r.Body).Decode(&sdp); err != nil {
				fmt.Fprint(os.Stderr, err)
				return
			}
			sdpPayload := &model.SDPPayload{
				SDP: &sdp,
				ID:  RandSeq(IDLength),
			}

			fmt.Printf("Received SDP from answer peer %v\n", sdpPayload.ID)
			receiver.answers[len(receiver.answers)-1] = sdpPayload
		case http.MethodGet:
			// Offer asking for Answer's SDP
			if len(receiver.answers) == 0 {
				return
			}
			sdpPayload := receiver.answers[len(receiver.answers)-1]
			if err := json.NewEncoder(w).Encode(sdpPayload); err != nil {
				return
			}
			fmt.Printf("Sending SDP to offer peer %v\n", sdpPayload.ID)
		}
	})

	http.HandleFunc(HTTPICEPath, func(w http.ResponseWriter, r *http.Request) {
		receiver.peersMux.Lock()
		defer receiver.peersMux.Unlock()

		fmt.Printf("Received %v on ICEInit endpoint for %v\n", r.Method, r.RemoteAddr)

		switch r.Method {
		case http.MethodPost:
			id := r.URL.Query().Get("id")

			b, err := io.ReadAll(r.Body)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error while extracting ICEInit for id %v\n", id)
				return
			}
			r.Body.Close()

			var iceCandidates []webrtc.ICECandidateInit

			err = json.Unmarshal(b, &iceCandidates)
			if err != nil {
				fmt.Fprintf(os.Stderr, "unmarshaling for id %v\n", id)
			}

			receiver.peerICECandidates[id] = iceCandidates

			fmt.Printf("Received %v ICE Candidates from %v\n", len(iceCandidates), id)
		case http.MethodGet:
			id := r.URL.Query().Get("id")

			connectedId := receiver.connectedPeers[id]
			if err := json.NewEncoder(w).Encode(receiver.peerICECandidates[connectedId]); err != nil {
				fmt.Fprintf(os.Stderr, "marshalling for id %v\n", id)
				return
			}

			fmt.Printf("peer %v is connected to peer %v, sent connected peer's candidates", id, connectedId)

			fmt.Printf("Sent %v ICE Candidates to %v\n", len(receiver.peerICECandidates[connectedId]), connectedId)
		}
	})
}

func (receiver *Coordinator) Listen() {
	fmt.Printf("Coordinator has started listening\n")
	receiver.handleRegister()
	receiver.handleSdp()
	http.ListenAndServe(fmt.Sprintf(":%d", receiver.port), nil)
}

func (receiver *Coordinator) StopListening(val bool) {
	receiver.awaitChannel <- val
}
