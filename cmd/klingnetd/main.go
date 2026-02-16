// Klingnet full node daemon.
//
// Usage:
//
//	klingnetd [--mine --validator-key=...] Run node
//	klingnetd --help                       Show help
package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Klingon-tech/klingnet-chain/config"
	"github.com/Klingon-tech/klingnet-chain/internal/node"
)

func main() {
	cfg, _, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	n, err := node.New(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if err := n.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		n.Stop()
		os.Exit(1)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	n.Stop()
}
