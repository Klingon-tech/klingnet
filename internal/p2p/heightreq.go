package p2p

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

const (
	// HeightProtocol is the protocol ID for querying chain height.
	HeightProtocol = protocol.ID("/klingnet/height/1.0.0")

	// heightReadTimeout is the max time to read a height response.
	heightReadTimeout = 5 * time.Second
)

// HeightResponse contains a peer's chain height and tip hash.
type HeightResponse struct {
	Height  uint64 `json:"height"`
	TipHash string `json:"tip_hash"`
}

// RegisterHeightHandler registers a stream handler that responds with the
// local chain height and tip hash.
func (s *Syncer) RegisterHeightHandler(heightFn func() (uint64, string)) {
	s.host.SetStreamHandler(HeightProtocol, func(stream network.Stream) {
		defer stream.Close()

		height, tipHash := heightFn()
		resp := HeightResponse{Height: height, TipHash: tipHash}
		json.NewEncoder(stream).Encode(&resp)
	})
}

// RequestHeight queries a peer for its chain height and tip hash.
func (s *Syncer) RequestHeight(ctx context.Context, peerID peer.ID) (*HeightResponse, error) {
	return s.requestHeight(ctx, peerID, HeightProtocol)
}

// RegisterSubChainHeightHandler registers a height provider for a sub-chain.
func (s *Syncer) RegisterSubChainHeightHandler(chainIDHex string, heightFn func() (uint64, string)) {
	s.host.SetStreamHandler(SubChainHeightProtocol(chainIDHex), func(stream network.Stream) {
		defer stream.Close()

		height, tipHash := heightFn()
		resp := HeightResponse{Height: height, TipHash: tipHash}
		json.NewEncoder(stream).Encode(&resp)
	})
}

// RequestSubChainHeight queries a peer for a sub-chain's height and tip hash.
func (s *Syncer) RequestSubChainHeight(ctx context.Context, peerID peer.ID, chainIDHex string) (*HeightResponse, error) {
	return s.requestHeight(ctx, peerID, SubChainHeightProtocol(chainIDHex))
}

// requestHeight is the shared implementation for height queries.
func (s *Syncer) requestHeight(ctx context.Context, peerID peer.ID, proto protocol.ID) (*HeightResponse, error) {
	stream, err := s.host.NewStream(ctx, peerID, proto)
	if err != nil {
		return nil, fmt.Errorf("open height stream: %w", err)
	}
	defer stream.Close()

	// Signal we're done writing (request is empty, just opening the stream).
	stream.CloseWrite()

	_ = stream.SetReadDeadline(time.Now().Add(heightReadTimeout))

	var resp HeightResponse
	if err := json.NewDecoder(io.LimitReader(stream, 1024)).Decode(&resp); err != nil {
		return nil, fmt.Errorf("read height response: %w", err)
	}

	return &resp, nil
}
