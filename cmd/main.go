package main

import (
	"flag"
	"fmt"
	"time"
	"webrtc-playground/internal/operator/coordinator"
	"webrtc-playground/internal/operator/peer"
)

const (
	NODE_TYPE_PEER  = "PEER"
	NODE_TYPE_COORD = "COORD"
)

func main() {
	var nodeType string
	var coordAddress string
	coordPort := flag.Int("coordinator_port", -1, "Port on which coordinator runs, mandatory field")
	flag.StringVar(&nodeType, "node_type", "", "Determines which logic should node enforce, mandatory field")
	flag.StringVar(&coordAddress, "coordinator_address", "", "Address for coordinator node, mandatory field")

	if nodeType == "" || coordAddress == "" || *coordPort == -1 {
		panic(fmt.Errorf("Mandatory fields were missing, please check -h"))
	}

	flag.Parse()

	switch nodeType {
	case NODE_TYPE_PEER:
		// Await for some arbitrary duration to let coordinator node start up
		time.Sleep(30 * time.Second)

		peerNode, err := peer.New(coordAddress, *coordPort)
		if err != nil {
			panic(err)
		}
		err = peerNode.InitConnection()
		if err != nil {
			panic(err)
		}
		peerNode.SendData()
		peerNode.Await()

		peerNode.Await()
	case NODE_TYPE_COORD:
		coordNode, err := coordinator.New(*coordPort)
		if err != nil {
			panic(err)
		}
		coordNode.Listen()
	}
}
