/*
Copyright © 2024 Luka Tchumburidze <lukatchumburidze@gmail.com>

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <http://www.gnu.org/licenses/>.
*/
package cmd

import (
	"fmt"
	"log"
	"webrtc-playground/internal/worker"

	"github.com/spf13/cobra"
)

var randMessageCmdConfig = RandMessagePeerCmdConfig{}

type RandMessagePeerCmdConfig struct {
	PeerCmdConfig
	NOfMessages uint
}

// randMessageCmd represents the randMessage command
var randMessageCmd = &cobra.Command{
	Use:   "randMessage",
	Short: "Generates arbitrary number of random messages",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Starting random message worker")
		worker, err := worker.NewRandMessageWorker(randMessageCmdConfig.NOfMessages)
		if err != nil {
			log.Fatal(err)
		}
		workerCmdRun(randMessageCmdConfig.PeerCmdConfig, worker)
	},
}

func init() {
	peerCmd.AddCommand(randMessageCmd)

	randMessageCmd.Flags().UintVar(&randMessageCmdConfig.NOfMessages, "n_of_messages", 5, "Number of random messages to generate")
}
