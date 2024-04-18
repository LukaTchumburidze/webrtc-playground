package cmd

import (
	"log"
	"time"
	"webrtc-playground/internal/operator/peer"
	"webrtc-playground/internal/worker"
)

const peerTimeout = 5 * time.Second

func workerCmdRun(peerConfig PeerCmdConfig, worker worker.Worker) {
	log.Printf("Peer has been started, waiting for %v\n", peerTimeout)
	time.Sleep(peerTimeout)

	peerNode, err := peer.New(peerConfig.CoordinatorAddress, peerCmdConfig.CoordinatorPort, &worker)
	if err != nil {
		log.Fatal(err)
	}

	if err := peerNode.InitConnection(); err != nil {
		log.Fatal(err)
	}

	if err := peerNode.Await(); err != nil {
		log.Fatal(err)
	}

	log.Printf("Peer Node completed successfully\n")
}
