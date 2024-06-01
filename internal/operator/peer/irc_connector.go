package peer

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/pion/webrtc/v3"
	"github.com/sirupsen/logrus"
	"os"
	"strings"
	"webrtc-playground/internal/logger"
	"webrtc-playground/internal/model"
	"webrtc-playground/internal/operator/coordinator"

	irc "github.com/fluffle/goirc/client"
)

const (
	ircServerName    = "irc.freenode.net"
	ircServerAddress = "irc.freenode.net:7000"
	ircChannelName   = "#lt-webrtc-playground"
	ircNamePrefix    = "webrtc-bot-"

	ircPeerRoleOffer  = "offer"
	ircPeerRoleAnswer = "answer"
	ircPeerRoleNone   = ""

	fragmentLen       = 400
	fragmentSeparator = '.'
)

var (
	errFragmentIncorrectLen = errors.New("fragment has incorrect length")
)

type pvtMsgPayload struct {
	PeerRole   string           `json:"peer_role"`
	SDPPayload model.SDPPayload `json:"sdp_payload"`
	ICEPayload model.ICEPayload `json:"ice_payload"`
}

type ircPeerConnector struct {
	ircServerPath  string
	ircChannelName string
	ircUsername    string

	client        *irc.Conn
	ircDisconnect chan bool
	ircQuitSignal chan os.Signal

	sdpChannel     chan model.SDPPayload
	coPeerUsername string
	whoEnded       bool
	fragmenter     msgFragmenter
}

type msgFragmenter struct {
	fragments []string
}

func (mf *msgFragmenter) addFragment(frag string) (bool, error) {
	if frag[len(frag)-1] == fragmentSeparator && len(frag) != fragmentLen+1 {
		return false, errFragmentIncorrectLen
	}
	mf.fragments = append(mf.fragments, frag)
	if frag[len(frag)-1] != fragmentSeparator {
		return true, nil
	} else {
		return false, nil
	}
}

func (mf *msgFragmenter) getFragments(text string) []string {
	fragments := make([]string, 0)
	start := 0
	for start < len(text) {
		end := start + fragmentLen
		if end > len(text) {
			end = len(text) - 1
		} else {
			text = text[:end] + string(fragmentSeparator) + text[end:]
		}
		fragments = append(fragments, text[start:end+1])
		start = end + 1
	}
	return fragments
}

func (mf *msgFragmenter) joinFragments() string {
	rawFragment := strings.Join(mf.fragments, "")
	trimmedFragment := strings.ReplaceAll(rawFragment, string(fragmentSeparator), "")
	logger.Logger.WithField("joined_fragments", trimmedFragment).Info("joined fragments")
	return trimmedFragment
}

func newIRCPeerConnector() ircPeerConnector {
	return ircPeerConnector{
		ircServerPath:  ircServerAddress,
		ircChannelName: ircChannelName,
		ircUsername:    ircNamePrefix + coordinator.RandSeq(5),
		fragmenter:     msgFragmenter{fragments: make([]string, 0)},
	}
}

func (c ircPeerConnector) signalCandidates(id string, candidates []*webrtc.ICECandidateInit) error {
	for _, candidate := range candidates {
		encodedCandidate, err := json.Marshal(pvtMsgPayload{
			PeerRole: ircPeerRoleNone,
			ICEPayload: model.ICEPayload{
				ICEInit: *candidate,
				ID:      id,
			},
		})
		if err != nil {
			return err
		}
		encodedCandidateString := base64.StdEncoding.EncodeToString(encodedCandidate)
		c.client.Privmsg(c.coPeerUsername, encodedCandidateString)
		logger.Logger.WithField("candidate", candidate).Debug("sent candidate")
	}

	return nil
}

func (c ircPeerConnector) addIRCHandlers(peer *Peer) error {
	c.client.HandleFunc(irc.CONNECTED,
		func(conn *irc.Conn, line *irc.Line) { conn.Join(c.ircChannelName) })
	// And a signal on disconnect
	c.client.HandleFunc(irc.DISCONNECTED,
		func(conn *irc.Conn, line *irc.Line) { c.ircDisconnect <- true })

	c.client.HandleFunc(irc.ACTION, func(conn *irc.Conn, line *irc.Line) {
		logger.Logger.WithField("action", line.Text()).Info("received action")
	})

	c.client.HandleFunc(irc.AUTHENTICATE, func(conn *irc.Conn, line *irc.Line) {
		logger.Logger.WithField("authenticate", line.Text()).Info("received authenticate")
	})

	// WHO code
	c.client.HandleFunc("352",
		func(conn *irc.Conn, l *irc.Line) {
			if c.whoEnded {
				return
			}
			otherName := l.Args[5]
			if otherName == c.ircUsername {
				return
			}
			if strings.HasPrefix(otherName, ircNamePrefix) && c.coPeerUsername == "" {
				c.coPeerUsername = otherName
				logger.Logger.WithFields(logrus.Fields{
					"peer-irc-username":    c.ircUsername,
					"co-peer-irc-username": c.coPeerUsername,
				}).Info("co-peer has been chosen")
			}
		})

	// Received an END OF WHO reply.
	c.client.HandleFunc("315",
		func(conn *irc.Conn, l *irc.Line) {
			c.whoEnded = true
			if c.coPeerUsername != "" {
				offerSdp, err := peer.PeerConnection.CreateOffer(nil)
				if err != nil {
					logger.Logger.WithError(err).Error("could not create offer sdp")
				}
				err = peer.PeerConnection.SetLocalDescription(offerSdp)
				if err != nil {
					logger.Logger.WithError(err).Error("")
				}
				b, err := json.Marshal(pvtMsgPayload{
					PeerRole: ircPeerRoleOffer,
					SDPPayload: model.SDPPayload{
						SDP: &offerSdp,
					},
				})
				if err != nil {
					logger.Logger.WithError(err).Error("could not marshal offer remote description")
				}
				encodedPayload := base64.StdEncoding.EncodeToString(b)
				frags := c.fragmenter.getFragments(encodedPayload)
				for _, el := range frags {
					logger.Logger.WithFields(logrus.Fields{
						"text":     el,
						"text_len": len(el),
					}).Debug("sending pvt msg")
					c.client.Privmsg(c.coPeerUsername, el)
				}
			} else {
				logger.Logger.Info("there are no peers in channel, waiting for other peer to join")
			}

			logger.Logger.Info("End of /WHO list.")
		})

	c.client.HandleFunc(irc.PRIVMSG,
		func(conn *irc.Conn, line *irc.Line) {
			msg := line.Text()
			logger.Logger.WithFields(logrus.Fields{
				"len":      len(line.Raw),
				"text":     line.Text(),
				"text_len": len(line.Text()),
			}).Debug("received pvt msg")
			isLast, err := c.fragmenter.addFragment(msg)
			if err != nil {
				logger.Logger.WithError(err).Error("could not add fragment")
			}
			if !isLast {
				return
			}
			msg = c.fragmenter.joinFragments()
			c.fragmenter.fragments = make([]string, 0)

			var payload pvtMsgPayload
			decodedMsg, err := base64.StdEncoding.DecodeString(msg)
			if err != nil {
				logger.Logger.WithError(err).Error("Failed to decode Base64 message")
				return
			}
			err = json.Unmarshal(decodedMsg, &payload)
			if err != nil {
				logger.Logger.Error("could not unmarshal private msg")
				return
			}
			switch payload.PeerRole {
			case ircPeerRoleNone:
				// ICE candidate received
				peer.candidatesMux.Lock()
				err = peer.PeerConnection.AddICECandidate(payload.ICEPayload.ICEInit)
				if err != nil {
					logger.Logger.WithError(err).Error("could not add ICECandidate")
				}
				peer.candidatesMux.Unlock()
			case ircPeerRoleOffer:
				c.coPeerUsername = line.Nick
				err = peer.PeerConnection.SetRemoteDescription(*payload.SDPPayload.SDP)
				if err != nil {
					logger.Logger.WithError(err).Error("could not set offer remote description for answer peer")
					return
				}
				answerSdp, err := peer.PeerConnection.CreateAnswer(nil)
				if err != nil {
					logger.Logger.WithError(err).Error("could not create answer remote description for answer peer")
					return
				}
				err = peer.PeerConnection.SetLocalDescription(answerSdp)
				if err != nil {
					logger.Logger.WithError(err).Error("could not set offer local description for answer peer")
					return
				}
				b, err := json.Marshal(pvtMsgPayload{
					PeerRole: ircPeerRoleAnswer,
					SDPPayload: model.SDPPayload{
						SDP: &answerSdp,
					},
				})
				if err != nil {
					logger.Logger.WithError(err).Error("could not marshal answer remote description")
					return
				}
				encodedPayload := base64.StdEncoding.EncodeToString(b)
				frags := c.fragmenter.getFragments(encodedPayload)
				for _, el := range frags {
					logger.Logger.WithFields(logrus.Fields{
						"text":     el,
						"text_len": len(el),
					}).Debug("sending pvt msg")
					c.client.Privmsg(c.coPeerUsername, el)
				}
				peer.id = payload.SDPPayload.ID
				err = c.signalCandidates(peer.id, peer.pendingCandidates)
				if err != nil {
					logger.Logger.WithError(err).Error("could not signal candidates")
				}
			case ircPeerRoleAnswer:
				err = peer.PeerConnection.SetRemoteDescription(*payload.SDPPayload.SDP)
				if err != nil {
					logger.Logger.WithError(err).Error("could not set answer remote description for offer peer")
					return
				}
				peer.id = payload.SDPPayload.ID
				err = c.signalCandidates(peer.id, peer.pendingCandidates)
				if err != nil {
					logger.Logger.WithError(err).Error("could not signal candidates")
				}
			}

			logger.Logger.WithField("nick", line.Nick).Debug(line.Text())
		})

	c.client.HandleFunc(irc.JOIN, func(conn *irc.Conn, line *irc.Line) {
		c.client.Mode(c.ircUsername, "-R")
		logger.Logger.WithField("line", line).Info("received join")
	})
	c.client.HandleFunc(irc.WHO, func(conn *irc.Conn, line *irc.Line) {
		logger.Logger.WithField("line", line).Info("received who")

	})

	return nil
}

func (c ircPeerConnector) stop() {
	c.ircDisconnect <- true
}

func (c ircPeerConnector) connectPeers(peer *Peer) error {
	cfg := irc.NewConfig(c.ircUsername)
	cfg.SSL = true
	cfg.SSLConfig = &tls.Config{ServerName: ircServerName}
	cfg.Server = ircServerAddress
	cfg.NewNick = func(n string) string { return n + "-new" }

	c.client = irc.Client(cfg)
	c.client.EnableStateTracking()

	peer.id = coordinator.RandSeq(coordinator.IDLength)

	err := c.addIRCHandlers(peer)
	if err != nil {
		return err
	}
	err = c.client.Connect()
	if err != nil {
		return err
	}

outer:
	for {
		select {
		case sig := <-c.ircQuitSignal:
			logger.Logger.WithField("signal", sig).Info("Received signal, shutting down")
			close(c.ircDisconnect)
		case <-c.ircDisconnect:
			logger.Logger.Info("Disconnected")
			err := c.client.Close()
			if err != nil {
				return err
			}
			break outer
		}
	}

	logger.Logger.Info("peer connector stopped")
	return nil
}
