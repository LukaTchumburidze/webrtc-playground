package config

type PeerCmdConfig struct {
	SDPExchangeMethod  string
	CoordinatorAddress string
	CoordinatorPort    uint16
}

type RandMessagePeerCmdConfig struct {
	PeerCmdConfig
	NOfMessages uint
}

type FileSystemPeerCmdConfig struct {
	PeerCmdConfig
	Directory string
	Recursive bool
}

type ChatPeerCmdConfig struct {
	PeerCmdConfig
	Username string
}

type CoordinatorConfig struct {
	Port uint16
}
