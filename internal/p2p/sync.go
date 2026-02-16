package p2p

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/Klingon-tech/klingnet-chain/pkg/block"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

const (
	// SyncProtocol is the protocol ID for chain synchronization.
	SyncProtocol = protocol.ID("/klingnet/sync/1.0.0")

	// syncReadTimeout is the max time to read a sync response.
	syncReadTimeout = 30 * time.Second

	// maxSyncResponseBytes limits sync response size (10 MB).
	maxSyncResponseBytes = 10 * 1024 * 1024
)

// SyncRequest asks a peer for blocks starting at a given height.
type SyncRequest struct {
	FromHeight uint64 `json:"from_height"`
	MaxBlocks  uint32 `json:"max_blocks"`
}

// SyncResponse contains blocks returned by a peer.
type SyncResponse struct {
	Blocks []*block.Block `json:"blocks"`
}

// Syncer handles chain synchronization with peers.
type Syncer struct {
	node *Node
	host host.Host

	// BlockHandler processes blocks received during sync.
	BlockHandler func(*block.Block) error
}

// NewSyncer creates a new chain syncer attached to the given node.
func NewSyncer(node *Node) *Syncer {
	return &Syncer{
		node: node,
		host: node.host,
	}
}

// RegisterHandler registers the sync stream handler on the host.
// The provider function returns blocks for a given height range.
func (s *Syncer) RegisterHandler(provider func(fromHeight uint64, max uint32) []*block.Block) {
	s.host.SetStreamHandler(SyncProtocol, func(stream network.Stream) {
		defer stream.Close()

		var req SyncRequest
		if err := json.NewDecoder(io.LimitReader(stream, maxSyncResponseBytes)).Decode(&req); err != nil {
			return
		}

		if req.MaxBlocks == 0 || req.MaxBlocks > 500 {
			req.MaxBlocks = 500
		}

		blocks := provider(req.FromHeight, req.MaxBlocks)
		resp := SyncResponse{Blocks: blocks}
		json.NewEncoder(stream).Encode(&resp)
	})
}

// RequestBlocks asks a specific peer for blocks starting at fromHeight.
func (s *Syncer) RequestBlocks(ctx context.Context, peerID peer.ID, fromHeight uint64, maxBlocks uint32) ([]*block.Block, error) {
	return s.requestBlocks(ctx, peerID, SyncProtocol, fromHeight, maxBlocks)
}

// RegisterSubChainHandler registers a block-provider for a sub-chain sync protocol.
func (s *Syncer) RegisterSubChainHandler(chainIDHex string, provider func(uint64, uint32) []*block.Block) {
	s.host.SetStreamHandler(SubChainSyncProtocol(chainIDHex), func(stream network.Stream) {
		defer stream.Close()

		var req SyncRequest
		if err := json.NewDecoder(io.LimitReader(stream, maxSyncResponseBytes)).Decode(&req); err != nil {
			return
		}

		if req.MaxBlocks == 0 || req.MaxBlocks > 500 {
			req.MaxBlocks = 500
		}

		blocks := provider(req.FromHeight, req.MaxBlocks)
		resp := SyncResponse{Blocks: blocks}
		json.NewEncoder(stream).Encode(&resp)
	})
}

// RemoveSubChainHandler removes sync and height stream handlers for a sub-chain.
func (s *Syncer) RemoveSubChainHandler(chainIDHex string) {
	s.host.RemoveStreamHandler(SubChainSyncProtocol(chainIDHex))
	s.host.RemoveStreamHandler(SubChainHeightProtocol(chainIDHex))
}

// RequestSubChainBlocks asks a peer for blocks from a specific sub-chain.
func (s *Syncer) RequestSubChainBlocks(ctx context.Context, peerID peer.ID, chainIDHex string, fromHeight uint64, maxBlocks uint32) ([]*block.Block, error) {
	return s.requestBlocks(ctx, peerID, SubChainSyncProtocol(chainIDHex), fromHeight, maxBlocks)
}

// requestBlocks is the shared implementation for block requests.
func (s *Syncer) requestBlocks(ctx context.Context, peerID peer.ID, proto protocol.ID, fromHeight uint64, maxBlocks uint32) ([]*block.Block, error) {
	stream, err := s.host.NewStream(ctx, peerID, proto)
	if err != nil {
		return nil, fmt.Errorf("open sync stream: %w", err)
	}
	defer stream.Close()

	req := SyncRequest{FromHeight: fromHeight, MaxBlocks: maxBlocks}
	if err := json.NewEncoder(stream).Encode(&req); err != nil {
		return nil, fmt.Errorf("send sync request: %w", err)
	}

	// Signal we're done writing.
	stream.CloseWrite()

	// Read response with timeout.
	_ = stream.SetReadDeadline(time.Now().Add(syncReadTimeout))

	var resp SyncResponse
	if err := json.NewDecoder(io.LimitReader(stream, maxSyncResponseBytes)).Decode(&resp); err != nil {
		return nil, fmt.Errorf("read sync response: %w", err)
	}

	return resp.Blocks, nil
}
