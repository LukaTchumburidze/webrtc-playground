package coordinator

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/pion/randutil"
	"github.com/pion/webrtc/v3"
	"github.com/sirupsen/logrus"
	"io"
	"net/http"
	"sync"
	"time"
	"webrtc-playground/internal/logger"
	"webrtc-playground/internal/model"
)

const (
	StatusBusy = model.RoleStatus("BUSY")
	RoleOffer  = model.RoleStatus("OFFER")
	RoleAnswer = model.RoleStatus("ANSWER")

	HTTPICEPath          = "/ice"
	HttpRegisterPath     = "/register"
	HttpSDPOfferPath     = "/sdp/offer"
	HttpSDPAnswerPath    = "/sdp/answer"
	HttpRegisterDonePath = "/register/done"

	MatchingTimeout = 60 * time.Second
)

var (
	ErrNoSDPToSend = errors.New("there's no offer SDP to send")
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

		logger.Logger.WithField("address", r.RemoteAddr).Info("Registration received")

		if receiver.isBusy {
			buff := bytes.Buffer{}
			err := json.NewEncoder(&buff).Encode(model.RoleStatus(StatusBusy))
			if err != nil {
				logger.Logger.WithError(err).Error("error while encoding role")
				return
			}
			_, err = w.Write(buff.Bytes())
			if err != nil {
				logger.Logger.WithError(err).Error("error while writing response")
				return
			}

			return
		}

		if len(receiver.offers) == len(receiver.answers) {
			buff := bytes.Buffer{}
			err := json.NewEncoder(&buff).Encode(model.RoleStatus(RoleOffer))
			if err != nil {
				logger.Logger.WithError(err).Error("error while encoding role")
				return
			}
			_, err = w.Write(buff.Bytes())
			if err != nil {
				logger.Logger.WithError(err).Error("error while writing response")
				return
			}

			receiver.offers = append(receiver.offers, nil)
		} else {
			buff := bytes.Buffer{}
			err := json.NewEncoder(&buff).Encode(model.RoleStatus(RoleAnswer))
			if err != nil {
				logger.Logger.WithError(err).Error("error while encoding role")
				return
			}
			_, err = w.Write(buff.Bytes())
			if err != nil {
				logger.Logger.WithError(err).Error("error while writing response")
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

		logger.Logger.WithFields(logrus.Fields{
			"offer_peer":  offerSDP.ID,
			"answer_peer": answerSDP.ID,
		}).Info("Registration done")
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
		logger.Logger.Info("After timeout both peers weren't registered, removing last 2 records from offers/answers")

		receiver.peersMux.Lock()

		receiver.offers = receiver.offers[:oldLen-1]
		receiver.answers = receiver.answers[:oldLen-1]
		receiver.isBusy = false

		receiver.peersMux.Unlock()
	}
}

func (receiver *Coordinator) handleSdp() {
	http.HandleFunc(HttpSDPOfferPath, func(w http.ResponseWriter, r *http.Request) {
		receiver.peersMux.Lock()
		defer receiver.peersMux.Unlock()

		logger.Logger.WithFields(logrus.Fields{
			"method":  r.Method,
			"address": r.RemoteAddr,
		}).Info("Received request on SDP_OFFER endpoint")

		switch r.Method {
		case http.MethodPost:
			// Offer giving us SDP
			sdp := webrtc.SessionDescription{}
			if err := json.NewDecoder(r.Body).Decode(&sdp); err != nil {
				logger.Logger.WithError(err).Error("error while decoding SDP")
				return
			}
			sdpPayload := &model.SDPPayload{
				ID:  RandSeq(IDLength),
				SDP: &sdp,
			}
			logger.Logger.WithField("id", sdpPayload.ID).Info("Received SDP from offer peer")
			receiver.offers[len(receiver.offers)-1] = sdpPayload

		case http.MethodGet:
			// Answer asking for offer's SDP
			if len(receiver.offers) == 0 {
				logger.Logger.WithError(ErrNoSDPToSend).Error("error while choosing error SDP")
				return
			}
			sdpPayload := receiver.offers[len(receiver.offers)-1]
			if err := json.NewEncoder(w).Encode(sdpPayload); err != nil {
				logger.Logger.WithError(err).Error("could not encode SDP")
				return
			}
			logger.Logger.WithField("id", sdpPayload.ID).Info("Sending SDPPayload to answer peer")
		}
	})

	http.HandleFunc(HttpSDPAnswerPath, func(w http.ResponseWriter, r *http.Request) {
		receiver.peersMux.Lock()
		defer receiver.peersMux.Unlock()

		logger.Logger.WithFields(logrus.Fields{
			"method":  r.Method,
			"address": r.RemoteAddr,
		}).Info("Received request on SDP_ANSWER endpoint")

		switch r.Method {
		case http.MethodPost:
			// Answer giving us SDP
			sdp := webrtc.SessionDescription{}
			if err := json.NewDecoder(r.Body).Decode(&sdp); err != nil {
				logger.Logger.WithError(err).Error("Could not decode SDP")
				return
			}
			sdpPayload := &model.SDPPayload{
				SDP: &sdp,
				ID:  RandSeq(IDLength),
			}

			logger.Logger.WithField("id", sdpPayload.ID).Info("Received SDP from answer peer")
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
			logger.Logger.WithField("id", sdpPayload.ID).Info("Sending SDP to offer peer")
		}
	})

	http.HandleFunc(HTTPICEPath, func(w http.ResponseWriter, r *http.Request) {
		receiver.peersMux.Lock()
		defer receiver.peersMux.Unlock()

		logger.Logger.WithFields(logrus.Fields{
			"method":  r.Method,
			"address": r.RemoteAddr,
		}).Info("Received request on ICEInit endpoint")

		switch r.Method {
		case http.MethodPost:
			id := r.URL.Query().Get("id")

			b, err := io.ReadAll(r.Body)
			if err != nil {
				logger.Logger.WithError(err).WithField("id", id).Error("Could not read request body")
				return
			}
			r.Body.Close()

			var iceCandidates []webrtc.ICECandidateInit

			err = json.Unmarshal(b, &iceCandidates)
			if err != nil {
				logger.Logger.WithError(err).WithField("id", id).Error("Could not unmarshal ICE candidates")
				return
			}

			receiver.peerICECandidates[id] = iceCandidates

			logger.Logger.WithFields(logrus.Fields{
				"id":              id,
				"n_of_candidates": len(iceCandidates),
			}).Info("Received ICE candidates")
		case http.MethodGet:
			id := r.URL.Query().Get("id")

			connectedId := receiver.connectedPeers[id]
			if err := json.NewEncoder(w).Encode(receiver.peerICECandidates[connectedId]); err != nil {
				logger.Logger.WithError(err).WithField("id", id).Error("Could not encode ICE candidates")
				return
			}

			logger.Logger.WithFields(logrus.Fields{
				"id":              id,
				"n_of_candidates": len(receiver.peerICECandidates[connectedId]),
			}).Info("ICE candidates has been sent")
			logger.Logger.WithFields(logrus.Fields{
				"first_id":  id,
				"second_id": connectedId,
			}).Info("peers ICE candidates has been exchanged")
		}
	})
}

func (receiver *Coordinator) Listen() {
	logger.Logger.Info("Coordinator has started listening")
	receiver.handleRegister()
	receiver.handleSdp()
	http.ListenAndServe(fmt.Sprintf(":%d", receiver.port), nil)
}

func (receiver *Coordinator) StopListening(val bool) {
	receiver.awaitChannel <- val
}
