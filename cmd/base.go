package cmd

import (
	"time"
	"webrtc-playground/config"
	"webrtc-playground/internal/logger"
	"webrtc-playground/internal/operator/peer"
	"webrtc-playground/internal/worker"
)

const peerTimeout = 5 * time.Second

func workerCmdRun(peerConfig config.PeerCmdConfig, worker worker.Worker) {
	logger.Logger.WithField("duration", peerTimeout).Info("Peer has been started, waiting")
	time.Sleep(peerTimeout)

	peerNode, err := peer.NewPeer(peerConfig, &worker)
	if err != nil {
		logger.Logger.Fatal(err)
	}

	if err := peerNode.InitConnection(); err != nil {
		logger.Logger.Fatal(err)
	}

	if err := peerNode.Await(); err != nil {
		logger.Logger.Fatal(err)
	}

	logger.Logger.Info("Peer Node completed successfully")
}
