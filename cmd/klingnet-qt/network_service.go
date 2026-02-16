package main

import (
	"github.com/Klingon-tech/klingnet-chain/internal/rpc"
)

// NetworkService exposes network and mempool queries to the frontend.
type NetworkService struct {
	app *App
}

// NodeInfo describes the local node.
type NodeInfo struct {
	ID    string   `json:"id"`
	Addrs []string `json:"addrs"`
}

// PeerEntry describes a connected peer.
type PeerEntry struct {
	ID          string `json:"id"`
	ConnectedAt string `json:"connected_at"`
}

// PeersInfo describes all connected peers.
type PeersInfo struct {
	Count int         `json:"count"`
	Peers []PeerEntry `json:"peers"`
}

// MempoolInfo describes the mempool state.
type MempoolInfo struct {
	Count  int    `json:"count"`
	MinFeeRate string `json:"min_fee_rate"`
}

// MempoolContent lists pending transaction hashes.
type MempoolContent struct {
	Hashes []string `json:"hashes"`
}

// GetNodeInfo returns the local node identity and addresses.
func (n *NetworkService) GetNodeInfo() (*NodeInfo, error) {
	var result rpc.NodeInfoResult
	if err := n.app.rpcClient().Call("net_getNodeInfo", nil, &result); err != nil {
		return nil, err
	}
	return &NodeInfo{ID: result.ID, Addrs: result.Addrs}, nil
}

// GetPeers returns connected peer information.
func (n *NetworkService) GetPeers() (*PeersInfo, error) {
	var result rpc.PeerInfoResult
	if err := n.app.rpcClient().Call("net_getPeerInfo", nil, &result); err != nil {
		return nil, err
	}

	peers := make([]PeerEntry, len(result.Peers))
	for i, p := range result.Peers {
		peers[i] = PeerEntry{ID: p.ID, ConnectedAt: p.ConnectedAt}
	}
	return &PeersInfo{Count: result.Count, Peers: peers}, nil
}

// GetMempoolInfo returns mempool statistics.
func (n *NetworkService) GetMempoolInfo() (*MempoolInfo, error) {
	var result rpc.MempoolInfoResult
	if err := n.app.rpcClient().Call("mempool_getInfo", nil, &result); err != nil {
		return nil, err
	}
	return &MempoolInfo{
		Count:  result.Count,
		MinFeeRate: formatAmount(result.MinFeeRate),
	}, nil
}

// GetMempoolContent returns the list of pending transaction hashes.
func (n *NetworkService) GetMempoolContent() (*MempoolContent, error) {
	var result rpc.MempoolContentResult
	if err := n.app.rpcClient().Call("mempool_getContent", nil, &result); err != nil {
		return nil, err
	}
	return &MempoolContent{Hashes: result.Hashes}, nil
}
