package config

// DefaultMainnet returns the default node configuration for mainnet.
func DefaultMainnet() *Config {
	return &Config{
		Network: Mainnet,
		DataDir: DefaultDataDir(),
		P2P: P2PConfig{
			Enabled:    true,
			ListenAddr: "0.0.0.0",
			Port:       30303,
			MaxPeers:   50,
			// Bootnodes are seed nodes that help new peers join the network.
			// Format: multiaddr strings, e.g.:
			//   "/ip4/203.0.113.1/tcp/30303/p2p/12D3KooW..."
			//   "/dns4/seed1.klingnet.io/tcp/30303/p2p/12D3KooW..."
			// Run seed nodes with --dht-server for optimal DHT performance.
			Seeds: []string{
				"/ip4/149.102.158.68/tcp/30303/p2p/12D3KooWSU83DfT1QXgji1XiC2d9RtkGFKt5zwG4sQQVN2R3nNGG",
			},
		},
		RPC: RPCConfig{
			Enabled:    true,
			Addr:       "127.0.0.1",
			Port:       8545,
			AllowedIPs: []string{"127.0.0.1"},
		},
		Wallet: WalletConfig{
			Enabled: false,
		},
		Mining: MiningConfig{
			Enabled: false,
			Threads: 1,
		},
		SubChainSync: SubChainSyncConfig{
			Mode: SubChainSyncNone,
		},
		Log: LogConfig{
			Level: "info",
			JSON:  false,
		},
	}
}

// DefaultTestnet returns the default node configuration for testnet.
func DefaultTestnet() *Config {
	cfg := DefaultMainnet()
	cfg.Network = Testnet
	cfg.P2P.Port = 30304
	cfg.RPC.Port = 8645
	return cfg
}

// Default returns the default node configuration for the given network.
func Default(network NetworkType) *Config {
	switch network {
	case Testnet:
		return DefaultTestnet()
	default:
		return DefaultMainnet()
	}
}
