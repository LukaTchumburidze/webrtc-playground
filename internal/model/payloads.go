package model

import (
	"github.com/pion/webrtc/v3"
)

type RoleStatus string

type RolePayload struct {
	Status RoleStatus
	ID     string
}

type SDPPayload struct {
	SDP *webrtc.SessionDescription
	ID  string
}

type ICEPayload struct {
	ICEInit *webrtc.ICECandidateInit
	ID      string
}
