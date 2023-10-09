package main

import (
	"flag"
	"fmt"
	"os"
	"time"
	"webrtc-playground/internal/operator/coordinator"
	"webrtc-playground/internal/operator/peer"
)

const (
	NODE_TYPE_PEER  = "PEER"
	NODE_TYPE_COORD = "COORD"
	PEER_TIMEOUT    = 5 * time.Second
)

func main() {
	var nodeType string
	var coordAddress string
	coordPort := flag.Int("coordinator_port", -1, "Port on which coordinator runs, mandatory field")
	flag.StringVar(&nodeType, "node_type", "", "Determines which logic should node enforce, mandatory field")
	flag.StringVar(&coordAddress, "coordinator_address", "", "Address for coordinator node, mandatory field")

	flag.Parse()
	if nodeType == "" || *coordPort == -1 {
		fmt.Fprintf(os.Stderr, "Mandatory fields were missing, please check -h\n")
		os.Exit(1)
	}

	switch nodeType {
	case NODE_TYPE_PEER:
		// Await for some arbitrary duration to let coordinator node start up
		fmt.Printf("Peer has been started, waiting for %v\n", PEER_TIMEOUT)
		time.Sleep(PEER_TIMEOUT)

		peerNode, err := peer.New(coordAddress, *coordPort)
		if err != nil {
			fmt.Fprint(os.Stderr, err)
			os.Exit(1)
		}
		err = peerNode.InitConnection()
		if err != nil {
			fmt.Fprint(os.Stderr, err)
			os.Exit(1)
		}

		err = peerNode.Await()
		if err != nil {
			fmt.Fprint(os.Stderr, err)
			os.Exit(1)
		}

		fmt.Printf("Peer Node completed successfully\n")
	case NODE_TYPE_COORD:
		coordNode, err := coordinator.New(*coordPort)
		if err != nil {
			panic(err)
		}
		coordNode.Listen()
	default:
		fmt.Fprintf(os.Stderr, "Node type is not correct, it can be following: [%v, %v]", NODE_TYPE_COORD, NODE_TYPE_PEER)
		os.Exit(1)
	}
}
