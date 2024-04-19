package model

import (
	"github.com/pion/webrtc/v3"
)

type RoleStatus string

type RolePayload struct {
	ID     string
	Status RoleStatus
}

type SDPPayload struct {
	ID  string
	SDP *webrtc.SessionDescription
}

type ICEPayload struct {
	ID      string
	ICEInit *webrtc.ICECandidateInit
}
