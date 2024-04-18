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

var fileSystemPeerCmdConfig = FileSystemPeerCmdConfig{}

type FileSystemPeerCmdConfig struct {
	PeerCmdConfig
	Directory string
	Recursive bool
}

// fsCmd represents the fs command
var fsCmd = &cobra.Command{
	Use:   "fs",
	Short: "Offers file system synchronization tool on specified directory among peers",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Starting file system worker")
		worker, err := worker.NewFSWorker(fileSystemPeerCmdConfig.Directory, fileSystemPeerCmdConfig.Recursive)
		if err != nil {
			log.Fatal(err)
		}
		workerCmdRun(fileSystemPeerCmdConfig.PeerCmdConfig, worker)
	},
}

func init() {
	peerCmd.AddCommand(fsCmd)

	randMessageCmd.Flags().StringVar(&fileSystemPeerCmdConfig.Directory, "directory", "./", "directory in which files should be synced")
	randMessageCmd.Flags().BoolVar(&fileSystemPeerCmdConfig.Recursive, "recursive", false, "whether synchronization should span subfolders")
}
