package model

import (
	"github.com/pion/webrtc/v3"
)

type Status string

type RolePayload struct {
	Status Status
	ID     string
}

type SDPPayload struct {
	Sdp *webrtc.SessionDescription
	ID  string
}

type ICEPayload struct {
	ICEInit *webrtc.ICECandidateInit
	ID      string
}
