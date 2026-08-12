package transport

type VPNKeysResponse struct {
	PrivateKey   string `json:"private_key"`
	PublicKey    string `json:"public_key"`
	PresharedKey string `json:"preshared_key"`
	Error        string `json:"error,omitempty"`
}

type SetupVPNRequest struct {
	ServiceMutationBinding
	Port int `json:"port"`
}

type SetupVPNResponse struct {
	Created bool   `json:"created"`
	Detail  string `json:"detail,omitempty"`
	Error   string `json:"error,omitempty"`
}

type VPNPeerSpec struct {
	PublicKey    string `json:"public_key"`
	PresharedKey string `json:"preshared_key"`
	IP           string `json:"ip"`
}

type SyncVPNPeersRequest struct {
	ServiceMutationBinding
	DesiredGeneration int64         `json:"desired_generation"`
	Peers             []VPNPeerSpec `json:"peers"`
}

type SyncVPNPeersResponse struct {
	Applied           bool   `json:"applied"`
	AppliedGeneration int64  `json:"applied_generation"`
	Error             string `json:"error,omitempty"`
}

type VPNPeerStat struct {
	PublicKey     string `json:"public_key"`
	LastHandshake int64  `json:"last_handshake"`
	RxBytes       int64  `json:"rx_bytes"`
	TxBytes       int64  `json:"tx_bytes"`
}

type VPNStatusResponse struct {
	Installed       bool          `json:"installed"`
	Configured      bool          `json:"configured"`
	Running         bool          `json:"running"`
	ServerPublicKey string        `json:"server_public_key,omitempty"`
	Port            int           `json:"port,omitempty"`
	Endpoint        string        `json:"endpoint,omitempty"`
	Peers           []VPNPeerStat `json:"peers,omitempty"`
	Error           string        `json:"error,omitempty"`
}
