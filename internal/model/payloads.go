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
	ID  string                     `json:"id"`
	SDP *webrtc.SessionDescription `json:"sdp"`
}

type ICEPayload struct {
	ID      string                  `json:"id"`
	ICEInit webrtc.ICECandidateInit `json:"ice_init"`
}
