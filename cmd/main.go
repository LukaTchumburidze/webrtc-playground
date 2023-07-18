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
)

func main() {
	var nodeType string
	var coordAddress string
	coordPort := flag.Int("coordinator_port", -1, "Port on which coordinator runs, mandatory field")
	flag.StringVar(&nodeType, "node_type", "", "Determines which logic should node enforce, mandatory field")
	flag.StringVar(&coordAddress, "coordinator_address", "", "Address for coordinator node, mandatory field")

	flag.Parse()
	if nodeType == "" || coordAddress == "" || *coordPort == -1 {
		fmt.Fprintf(os.Stderr, "Mandatory fields were missing, please check -h")
		os.Exit(1)
	}

	switch nodeType {
	case NODE_TYPE_PEER:
		// Await for some arbitrary duration to let coordinator node start up
		time.Sleep(30 * time.Second)

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
		peerNode.SendData()

		err = peerNode.Await()
		if err != nil {
			fmt.Fprint(os.Stderr, err)
			os.Exit(1)
		}

		fmt.Printf("Peer Node completed successfully")
	case NODE_TYPE_COORD:
		coordNode, err := coordinator.New(*coordPort)
		if err != nil {
			panic(err)
		}
		coordNode.Listen()
	}
}
