// Command testnet boots a 2-node local testnet from scratch.
//
// Usage: go run ./cmd/testnet/
//
// It generates a validator key, creates a genesis config, boots two in-process
// nodes (one block producer, one follower), produces 10 blocks with 3-second
// intervals, gossips them via libp2p, and verifies both chains converge.
// Ctrl+C for early shutdown.
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Klingon-tech/klingnet-chain/config"
	"github.com/Klingon-tech/klingnet-chain/internal/chain"
	"github.com/Klingon-tech/klingnet-chain/internal/consensus"
	klog "github.com/Klingon-tech/klingnet-chain/internal/log"
	"github.com/Klingon-tech/klingnet-chain/internal/mempool"
	"github.com/Klingon-tech/klingnet-chain/internal/miner"
	"github.com/Klingon-tech/klingnet-chain/internal/p2p"
	"github.com/Klingon-tech/klingnet-chain/internal/storage"
	"github.com/Klingon-tech/klingnet-chain/internal/subchain"
	"github.com/Klingon-tech/klingnet-chain/internal/utxo"
	"github.com/Klingon-tech/klingnet-chain/pkg/block"
	"github.com/Klingon-tech/klingnet-chain/pkg/crypto"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
	libp2ppeer "github.com/libp2p/go-libp2p/core/peer"
)

const (
	numBlocks = 10
	blockTime = 3 * time.Second
)

// nodeBundle groups all components for one logical node.
type nodeBundle struct {
	name      string
	chain     *chain.Chain
	pool      *mempool.Pool
	p2p       *p2p.Node
	miner     *miner.Miner      // nil for non-producers.
	scManager *subchain.Manager // nil if sub-chains disabled.
}

func main() {
	klog.Init("info", false, "")
	logger := klog.WithComponent("testnet")

	logger.Info().Msg("=== Klingnet 2-Node Local Testnet ===")

	// ── Phase 1: Load well-known testnet identity + Genesis ─────────────

	privKeyBytes, err := hex.DecodeString(config.TestnetValidatorPrivKey)
	if err != nil {
		logger.Fatal().Err(err).Msg("decode testnet private key")
	}
	validatorKey, err := crypto.PrivateKeyFromBytes(privKeyBytes)
	if err != nil {
		logger.Fatal().Err(err).Msg("load testnet validator key")
	}
	validatorPub := validatorKey.PublicKey()
	validatorAddr := crypto.AddressFromPubKey(validatorPub)
	pubHex := hex.EncodeToString(validatorPub)
	addrHex := hex.EncodeToString(validatorAddr[:])

	logger.Info().
		Str("validator_pub", pubHex[:16]+"...").
		Str("coinbase_addr", addrHex[:16]+"...").
		Msg("Using well-known testnet identity")

	gen := config.TestnetGenesis()
	gen.ChainID = "klingnet-testnet-local"
	gen.ChainName = "Local Testnet"
	gen.Timestamp = uint64(time.Now().Unix())

	validatorPubBytes, _ := hex.DecodeString(pubHex)
	validators := [][]byte{validatorPubBytes}

	logger.Info().Str("chain_id", gen.ChainID).Msg("Genesis config created")

	// ── Phase 2: Build Nodes ─────────────────────────────────────────────

	node1, err := buildNode("node-1", gen, validators, validatorKey, validatorAddr)
	if err != nil {
		logger.Fatal().Err(err).Msg("build node-1")
	}
	node2, err := buildNode("node-2", gen, validators, nil, types.Address{})
	if err != nil {
		logger.Fatal().Err(err).Msg("build node-2")
	}

	logger.Info().
		Uint64("node1_height", node1.chain.Height()).
		Uint64("node2_height", node2.chain.Height()).
		Msg("Genesis initialized on both nodes")

	// ── Phase 3: Start P2P + Connect ─────────────────────────────────────

	if err := node1.p2p.Start(); err != nil {
		logger.Fatal().Err(err).Msg("start node-1 p2p")
	}
	if err := node2.p2p.Start(); err != nil {
		logger.Fatal().Err(err).Msg("start node-2 p2p")
	}
	defer cleanup(node1, node2)

	logger.Info().
		Str("node1_id", node1.p2p.ID().String()[:16]+"...").
		Str("node2_id", node2.p2p.ID().String()[:16]+"...").
		Msg("P2P nodes started")

	connectNodes(node1.p2p, node2.p2p)
	time.Sleep(500 * time.Millisecond) // GossipSub mesh stabilization.

	logger.Info().
		Int("node1_peers", node1.p2p.PeerCount()).
		Int("node2_peers", node2.p2p.PeerCount()).
		Msg("Nodes connected")

	// ── Phase 4: Signal handling ─────────────────────────────────────────

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		logger.Info().Msg("Shutdown signal received")
		cancel()
	}()

	// ── Phase 5: Block production ────────────────────────────────────────

	logger.Info().
		Int("blocks", numBlocks).
		Dur("interval", blockTime).
		Msg("Starting block production")

	for i := 0; i < numBlocks; i++ {
		select {
		case <-ctx.Done():
			logger.Info().Msg("Production interrupted")
			goto verify
		default:
		}

		blk, err := node1.miner.ProduceBlock()
		if err != nil {
			logger.Fatal().Err(err).Msg("produce block")
		}

		if err := node1.chain.ProcessBlock(blk); err != nil {
			logger.Fatal().Err(err).Msg("process block on node-1")
		}
		node1.pool.RemoveConfirmed(blk.Transactions)

		if err := node1.p2p.BroadcastBlock(blk); err != nil {
			logger.Error().Err(err).Msg("broadcast block")
		}

		logger.Info().
			Uint64("height", blk.Header.Height).
			Str("hash", blk.Hash().String()[:16]+"...").
			Int("txs", len(blk.Transactions)).
			Uint64("reward", blk.Transactions[0].Outputs[0].Value).
			Msg("Block produced")

		if i < numBlocks-1 {
			select {
			case <-ctx.Done():
				goto verify
			case <-time.After(blockTime):
			}
		}
	}

verify:
	// ── Phase 6: Verification ────────────────────────────────────────────

	// Wait for last block to propagate.
	time.Sleep(2 * time.Second)

	h1 := node1.chain.Height()
	h2 := node2.chain.Height()
	t1 := node1.chain.TipHash()
	t2 := node2.chain.TipHash()

	logger.Info().
		Uint64("node1_height", h1).
		Uint64("node2_height", h2).
		Str("node1_tip", t1.String()[:16]+"...").
		Str("node2_tip", t2.String()[:16]+"...").
		Msg("Final chain state")

	if h1 == h2 && t1 == t2 {
		logger.Info().Msg("SUCCESS: Both nodes converged — chains match!")
		fmt.Println()
		fmt.Printf("  Blocks produced:  %d\n", h1)
		fmt.Printf("  Chain tip:        %s\n", t1)
		fmt.Printf("  Genesis alloc:    %d coins\n", gen.Alloc[addrHex]/config.Coin)
		fmt.Printf("  Block reward:     %.3f coins\n", float64(gen.Protocol.Consensus.BlockReward)/float64(config.Coin))
		fmt.Printf("  Min fee rate:     %d base units/byte\n", gen.Protocol.Consensus.MinFeeRate)
		fmt.Printf("  Max supply:       %d coins\n", gen.Protocol.Consensus.MaxSupply/config.Coin)
		fmt.Printf("  Decimals:         %d\n", config.Decimals)
		fmt.Println()
	} else {
		logger.Error().Msg("FAILURE: Chain mismatch between nodes!")
		os.Exit(1)
	}
}

// buildNode creates a fully wired node with chain, mempool, p2p, and optional miner.
func buildNode(name string, gen *config.Genesis, validators [][]byte,
	signerKey *crypto.PrivateKey, coinbaseAddr types.Address) (*nodeBundle, error) {

	db := storage.NewMemory()
	utxoStore := utxo.NewStore(db)

	poa, err := consensus.NewPoA(validators, gen.Protocol.Consensus.BlockTime)
	if err != nil {
		return nil, fmt.Errorf("create poa: %w", err)
	}
	if signerKey != nil {
		if err := poa.SetSigner(signerKey); err != nil {
			return nil, fmt.Errorf("set signer: %w", err)
		}
	}
	// Wire stake checker (genesis validators are exempt, so testnet works as-is).
	if gen.Protocol.Consensus.ValidatorStake > 0 {
		sc := consensus.NewUTXOStakeChecker(utxoStore, gen.Protocol.Consensus.ValidatorStake)
		poa.SetStakeChecker(sc)
	}

	ch, err := chain.New(types.ChainID{}, db, utxoStore, poa)
	if err != nil {
		return nil, fmt.Errorf("create chain: %w", err)
	}
	if err := ch.InitFromGenesis(gen); err != nil {
		return nil, fmt.Errorf("init genesis: %w", err)
	}

	adapter := miner.NewUTXOAdapter(utxoStore)
	pool := mempool.New(adapter, 5000)
	pool.SetMinFeeRate(gen.Protocol.Consensus.MinFeeRate)

	p2pNode := p2p.New(p2p.Config{
		ListenAddr: "127.0.0.1",
		Port:       0, // Random port.
		NoDiscover: true,
		NetworkID:  gen.ChainID,
	})

	// Wire handshake: verify peers are on the same chain.
	genesisHash, _ := gen.Hash()
	p2pNode.SetGenesisHash(genesisHash)
	p2pNode.SetHeightFn(func() uint64 { return ch.Height() })

	// Wire block handler: incoming gossip → process + cleanup mempool.
	nodeLogger := klog.WithComponent(name)
	p2pNode.SetBlockHandler(func(_ libp2ppeer.ID, data []byte) {
		var blk block.Block
		if err := json.Unmarshal(data, &blk); err != nil {
			nodeLogger.Error().Err(err).Msg("unmarshal block")
			return
		}
		if err := ch.ProcessBlock(&blk); err != nil {
			if !errors.Is(err, chain.ErrBlockKnown) {
				nodeLogger.Error().Err(err).Uint64("height", blk.Header.Height).Msg("process block")
			}
			return
		}
		pool.RemoveConfirmed(blk.Transactions)
		nodeLogger.Info().
			Uint64("height", blk.Header.Height).
			Str("hash", blk.Hash().String()[:16]+"...").
			Msg("Block received and applied")
	})

	var m *miner.Miner
	if signerKey != nil {
		m = miner.New(ch, poa, pool, coinbaseAddr,
			gen.Protocol.Consensus.BlockReward,
			gen.Protocol.Consensus.MaxSupply,
			ch.Supply)
	}

	// Wire sub-chain manager if enabled.
	var scMgr *subchain.Manager
	if gen.Protocol.SubChain.Enabled {
		scMgr, err = subchain.NewManager(subchain.ManagerConfig{
			ParentDB: db,
			ParentID: types.ChainID{},
			Rules:    &gen.Protocol.SubChain,
		})
		if err != nil {
			return nil, fmt.Errorf("create sub-chain manager: %w", err)
		}
		ch.SetRegistrationHandler(func(txHash types.Hash, idx uint32, value uint64, data []byte, height uint64) {
			if regErr := scMgr.HandleRegistration(txHash, idx, value, data, height); regErr != nil {
				nodeLogger.Warn().Err(regErr).Msg("Sub-chain registration failed")
			}
		})
	}

	return &nodeBundle{
		name:      name,
		chain:     ch,
		pool:      pool,
		p2p:       p2pNode,
		miner:     m,
		scManager: scMgr,
	}, nil
}

// connectNodes connects two P2P nodes directly.
func connectNodes(a, b *p2p.Node) {
	aHost := a.Host()
	info := libp2ppeer.AddrInfo{
		ID:    aHost.ID(),
		Addrs: aHost.Addrs(),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	b.Host().Connect(ctx, info)
}

// cleanup stops all P2P nodes.
func cleanup(nodes ...*nodeBundle) {
	for _, n := range nodes {
		n.p2p.Stop()
	}
}
