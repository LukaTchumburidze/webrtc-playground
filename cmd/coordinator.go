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
	"github.com/spf13/cobra"
	"webrtc-playground/internal/logger"
	"webrtc-playground/internal/operator/coordinator"
)

const (
	coordinatorPortDefaultValue uint16 = 3939
)

var coordinatorConfig = CoordinatorConfig{}

type CoordinatorConfig struct {
	Port uint16
}

// coordinatorCmd represents the coordinator command
var coordinatorCmd = &cobra.Command{
	Use:   "coordinator",
	Short: "Starts coordinator node which has very basic capabilities to connect 2 last connected peers with each other",
	Long:  `Starts coordinator node which has very basic capabilities to connect 2 last connected peers with each other`,
	Run: func(cmd *cobra.Command, args []string) {
		logger.Logger.Info("Executing coordinator command")
		coordinatorNode, err := coordinator.New(coordinatorConfig.Port)
		if err != nil {
			logger.Logger.Fatal(err)
		}
		coordinatorNode.Listen()
	},
}

func init() {
	rootCmd.AddCommand(coordinatorCmd)

	coordinatorCmd.Flags().Uint16Var(&coordinatorConfig.Port, "port", coordinatorPortDefaultValue, "Port on which coordinator node should listen to")
}
