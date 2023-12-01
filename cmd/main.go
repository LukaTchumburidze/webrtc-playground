package main

import (
	"flag"
	"fmt"
	"os"
	"time"
	"webrtc-playground/internal/mode"
	"webrtc-playground/internal/operator/coordinator"
	"webrtc-playground/internal/operator/peer"
)

const (
	NODE_TYPE_PEER  = "PEER"
	NODE_TYPE_COORD = "COORD"
	PEER_TIMEOUT    = 5 * time.Second
)

type WorkerFlags struct {
	Mode string
}

func setupWorkerFlags(workerFlags *WorkerFlags) {
	// TODO: setup worker flags
	flagDescription := fmt.Sprintf("Worker mode, can either be %v, %v or %v", mode.WORKER_RAND_MESSAGES, mode.WORKER_CHAT, mode.WORKER_FS)
	flag.StringVar(&workerFlags.Mode, "mode", mode.WORKER_RAND_MESSAGES, flagDescription)
}

func createWorkerFromFlags(workerFlags *WorkerFlags) (*mode.Worker, error) {
	// TODO: create appropriate worker from workerFlags
	return nil, nil
}

func main() {
	var nodeType, coordAddress string
	workerFlags := WorkerFlags{}

	coordPort := flag.Int("coordinator_port", -1, "Port on which coordinator runs, mandatory field")
	flag.StringVar(&nodeType, "node_type", "", "Determines which logic should node enforce, mandatory field")
	flag.StringVar(&coordAddress, "coordinator_address", "", "Address for coordinator node, mandatory field")

	setupWorkerFlags(&workerFlags)

	flag.Parse()
	if nodeType == "" || *coordPort == -1 {
		fmt.Fprintf(os.Stderr, "Mandatory fields were missing, please check -h\n")
		os.Exit(1)
	}

	worker, err := createWorkerFromFlags(&workerFlags)
	if err == nil {
		fmt.Fprint(os.Stderr, err)
		os.Exit(1)
	}

	switch nodeType {
	case NODE_TYPE_PEER:
		// Await for some arbitrary duration to let coordinator node start up
		fmt.Printf("Peer has been started, waiting for %v\n", PEER_TIMEOUT)
		time.Sleep(PEER_TIMEOUT)

		peerNode, err := peer.New(coordAddress, *coordPort, worker)
		if err != nil {
			fmt.Fprint(os.Stderr, err)
			os.Exit(1)
		}

		if err := peerNode.InitConnection(); err != nil {
			fmt.Fprint(os.Stderr, err)
			os.Exit(1)
		}

		if err := peerNode.Await(); err != nil {
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
