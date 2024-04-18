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
)

var peerCmdConfig = PeerCmdConfig{}

type PeerCmdConfig struct {
	SDPExchangeMethod  string
	CoordinatorAddress string
	CoordinatorPort    uint16
}

// peerCmd represents the peer command
var peerCmd = &cobra.Command{
	Use:   "peer",
	Short: "Starts peer node which tries to exchange sdp with other peer and perform some arbitrary work",
	Long: `Peer node is a webrtc worker which can be configured through flags, at first
it tries to establish base communication mechanism with either irc channel
or coordinator node and through them exchange webrtc SDP with other peer
to proceed work specified with one of the subcommands`,
}

func init() {
	rootCmd.AddCommand(peerCmd)

	peerCmd.PersistentFlags().StringVar(&peerCmdConfig.SDPExchangeMethod, "sdp_exchange_method", "irc", "Which sdp exhange mechanism to use either coordinator or irc")
	peerCmd.PersistentFlags().StringVar(&peerCmdConfig.CoordinatorAddress, "coordinator_address", "", "address of coordinator node (used when sdp-exchange-method=coordinator)")
	peerCmd.PersistentFlags().Uint16Var(&peerCmdConfig.CoordinatorPort, "coordinator_port", coordinatorPortDefaultValue, "port of coordinator node (used when sdp-exchange-method=coordinator)")
}
