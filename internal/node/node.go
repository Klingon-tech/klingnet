// Package node provides a reusable blockchain node that can be embedded
// in any binary (daemon, Qt wallet, etc.).
package node

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Klingon-tech/klingnet-chain/config"
	"github.com/Klingon-tech/klingnet-chain/internal/chain"
	"github.com/Klingon-tech/klingnet-chain/internal/consensus"
	klog "github.com/Klingon-tech/klingnet-chain/internal/log"
	"github.com/Klingon-tech/klingnet-chain/internal/mempool"
	"github.com/Klingon-tech/klingnet-chain/internal/miner"
	"github.com/Klingon-tech/klingnet-chain/internal/p2p"
	"github.com/Klingon-tech/klingnet-chain/internal/rpc"
	"github.com/Klingon-tech/klingnet-chain/internal/storage"
	"github.com/Klingon-tech/klingnet-chain/internal/subchain"
	"github.com/Klingon-tech/klingnet-chain/internal/token"
	"github.com/Klingon-tech/klingnet-chain/internal/utxo"
	"github.com/Klingon-tech/klingnet-chain/internal/wallet"
	"github.com/Klingon-tech/klingnet-chain/pkg/block"
	"github.com/Klingon-tech/klingnet-chain/pkg/crypto"
	"github.com/Klingon-tech/klingnet-chain/pkg/tx"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/rs/zerolog"
)

// Node is a fully-initialized blockchain node.
type Node struct {
	cfg     *config.Config
	genesis *config.Genesis
	logger  zerolog.Logger

	// Core
	db         storage.DB
	utxoStore  *utxo.Store
	engine     consensus.Engine
	ch         *chain.Chain
	pool       *mempool.Pool
	tracker    *consensus.ValidatorTracker
	tokenStore *token.Store

	// Networking
	p2pNode *p2p.Node
	syncer  *p2p.Syncer

	// RPC
	rpcServer *rpc.Server

	// Mining
	validatorKey *crypto.PrivateKey
	poaEngine    *consensus.PoA

	// Sub-chains
	scManager *subchain.Manager
	scMinerMu sync.Mutex
	scMiners  map[types.ChainID]context.CancelFunc
	scHBMu    sync.Mutex
	scHBs     map[types.ChainID]context.CancelFunc

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New creates and initializes a new Node. It performs all setup steps
// (logger, genesis, storage, consensus, chain, mempool, P2P, RPC) but
// does NOT start background goroutines (mining, sync). Call Start() for that.
func New(cfg *config.Config) (*Node, error) {
	// ── 1. Set address HRP ──────────────────────────────────────────
	if cfg.Network == config.Testnet {
		types.SetAddressHRP(types.TestnetHRP)
	} else {
		types.SetAddressHRP(types.MainnetHRP)
	}

	// ── 2. Init logger ──────────────────────────────────────────────
	logFile := cfg.Log.File
	if logFile == "" {
		logsDir := cfg.LogsDir()
		if err := os.MkdirAll(logsDir, 0755); err != nil {
			return nil, fmt.Errorf("creating logs dir: %w", err)
		}
		logFile = logsDir + "/klingnet.log"
	}
	if err := klog.Init(cfg.Log.Level, cfg.Log.JSON, logFile); err != nil {
		return nil, fmt.Errorf("initializing logger: %w", err)
	}
	logger := klog.WithComponent("node")

	// ── 3. Genesis ──────────────────────────────────────────────────
	genesis := config.GenesisFor(cfg.Network)

	logger.Info().
		Str("chain_id", genesis.ChainID).
		Str("network", string(cfg.Network)).
		Str("consensus", genesis.Protocol.Consensus.Type).
		Int("block_time", genesis.Protocol.Consensus.BlockTime).
		Msg("Starting Klingnet Chain Node")

	// ── 4. Open storage ─────────────────────────────────────────────
	db, err := storage.NewBadger(cfg.ChainDataDir())
	if err != nil {
		return nil, fmt.Errorf("open database at %s: %w", cfg.ChainDataDir(), err)
	}

	utxoStore := utxo.NewStore(db)
	tokenStore := token.NewStore(db)
	logger.Info().Str("path", cfg.ChainDataDir()).Msg("Database opened")

	// ── 5. Validator key ────────────────────────────────────────────
	var validatorKey *crypto.PrivateKey
	if cfg.Mining.ValidatorKey != "" {
		validatorKey, err = loadValidatorKey(cfg.Mining.ValidatorKey)
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("load validator key %s: %w", cfg.Mining.ValidatorKey, err)
		}
		logger.Info().
			Str("pubkey", hex.EncodeToString(validatorKey.PublicKey())[:16]+"...").
			Msg("Validator key loaded")
	}
	if cfg.Mining.Enabled && validatorKey == nil {
		db.Close()
		return nil, fmt.Errorf("mining requires validator-key")
	}

	// ── 6. Consensus engine ─────────────────────────────────────────
	engine, err := createEngine(genesis)
	if err != nil {
		db.Close()
		if validatorKey != nil {
			validatorKey.Zero()
		}
		return nil, fmt.Errorf("create consensus engine: %w", err)
	}

	// Wire stake checker.
	if genesis.Protocol.Consensus.ValidatorStake > 0 {
		if poa, ok := engine.(*consensus.PoA); ok {
			sc := consensus.NewUTXOStakeChecker(utxoStore, genesis.Protocol.Consensus.ValidatorStake)
			poa.SetStakeChecker(sc)
			logger.Info().
				Uint64("min_stake", genesis.Protocol.Consensus.ValidatorStake).
				Msg("Validator staking enabled")
		}
	}

	// ── 7. Chain ────────────────────────────────────────────────────
	ch, err := chain.New(types.ChainID{}, db, utxoStore, engine)
	if err != nil {
		db.Close()
		if validatorKey != nil {
			validatorKey.Zero()
		}
		return nil, fmt.Errorf("create chain: %w", err)
	}
	ch.SetConsensusRules(genesis.Protocol.Consensus)

	state := ch.State()
	if state.IsGenesis() {
		if err := ch.InitFromGenesis(genesis); err != nil {
			db.Close()
			if validatorKey != nil {
				validatorKey.Zero()
			}
			return nil, fmt.Errorf("init from genesis: %w", err)
		}
		logger.Info().Msg("Chain initialized from genesis")
	} else {
		logger.Info().
			Uint64("height", ch.Height()).
			Str("tip", ch.TipHash().String()[:16]+"...").
			Msg("Chain resumed from database")
	}

	// ── 8. Mempool ──────────────────────────────────────────────────
	adapter := miner.NewUTXOAdapter(utxoStore)
	pool := mempool.New(adapter, 5000)
	pool.SetMinFeeRate(genesis.Protocol.Consensus.MinFeeRate)
	pool.SetCoinbaseMaturity(config.CoinbaseMaturity, ch.Height, utxoStore)
	pool.SetTokenValidator(&token.UTXOTokenAdapter{Set: utxoStore})
	pool.SetMintFee(config.TokenCreationFee)
	pool.SetStakeAmount(genesis.Protocol.Consensus.ValidatorStake)

	logger.Info().
		Uint64("min_fee_rate", genesis.Protocol.Consensus.MinFeeRate).
		Uint64("mint_fee", config.TokenCreationFee).
		Msg("Mempool ready")

	// ── 9. Validator tracker ────────────────────────────────────────
	tracker := consensus.NewValidatorTracker(60 * time.Second)

	var poaEngine *consensus.PoA
	if poa, ok := engine.(*consensus.PoA); ok {
		poaEngine = poa
	}

	// ── 10. P2P ─────────────────────────────────────────────────────
	var p2pNode *p2p.Node
	var syncer *p2p.Syncer
	var nodeRef *Node // set after Node is constructed; used by block handler closure
	if cfg.P2P.Enabled {
		p2pNode = p2p.New(p2p.Config{
			ListenAddr: cfg.P2P.ListenAddr,
			Port:       cfg.P2P.Port,
			Seeds:      cfg.P2P.Seeds,
			MaxPeers:   cfg.P2P.MaxPeers,
			NoDiscover: cfg.P2P.NoDiscover,
			DB:         db,
			DHTServer:  cfg.P2P.DHTServer,
			NetworkID:  genesis.ChainID,
			DataDir:    cfg.ChainDataDir(),
		})

		genesisHash, _ := genesis.Hash()
		p2pNode.SetGenesisHash(genesisHash)
		p2pNode.SetHeightFn(func() uint64 { return ch.Height() })

		// Block handler with sync trigger for unknown parents.
		var rootSyncing atomic.Bool
		p2pNode.SetBlockHandler(func(from peer.ID, data []byte) {
			var blk block.Block
			if err := json.Unmarshal(data, &blk); err != nil {
				logger.Debug().Err(err).Msg("Failed to unmarshal block")
				p2pNode.BanManager.RecordOffense(from, p2p.PenaltyInvalidBlock, "unmarshal: "+err.Error())
				return
			}
			if err := ch.ProcessBlock(&blk); err != nil {
				if errors.Is(err, chain.ErrPrevNotFound) && rootSyncing.CompareAndSwap(false, true) {
					go func() {
						defer rootSyncing.Store(false)
						if nodeRef != nil {
							nodeRef.runStartupSync()
						}
					}()
				}
				if !errors.Is(err, chain.ErrBlockKnown) &&
					!errors.Is(err, chain.ErrPrevNotFound) &&
					!errors.Is(err, chain.ErrForkDetected) {
					p2pNode.BanManager.RecordOffense(from, p2p.PenaltyInvalidBlock, err.Error())
				}
				if !errors.Is(err, chain.ErrBlockKnown) {
					logger.Debug().Err(err).Uint64("height", blk.Header.Height).Msg("Failed to process block")
				}
				return
			}
			pool.RemoveConfirmed(blk.Transactions)
			token.ExtractAndStoreMetadata(tokenStore, &blk)

			if poaEngine != nil {
				if signer := poaEngine.IdentifySigner(blk.Header); signer != nil {
					tracker.RecordBlock(signer)
				}
			}

			logger.Info().
				Uint64("height", blk.Header.Height).
				Str("hash", blk.Hash().String()[:16]+"...").
				Int("txs", len(blk.Transactions)).
				Msg("Block received and applied")
		})

		// Tx handler.
		p2pNode.SetTxHandler(func(from peer.ID, data []byte) {
			var t tx.Transaction
			if err := json.Unmarshal(data, &t); err != nil {
				logger.Debug().Err(err).Msg("Failed to unmarshal transaction")
				p2pNode.BanManager.RecordOffense(from, p2p.PenaltyInvalidTx, "unmarshal: "+err.Error())
				return
			}
			fee, err := pool.Add(&t)
			if err != nil {
				logger.Debug().Err(err).Msg("Rejected transaction")
				p2pNode.BanManager.RecordOffense(from, p2p.PenaltyInvalidTx, err.Error())
				return
			}
			logger.Info().
				Str("tx", t.Hash().String()[:16]+"...").
				Uint64("fee", fee).
				Msg("Transaction added to mempool")
		})

		if err := p2pNode.Start(); err != nil {
			db.Close()
			if validatorKey != nil {
				validatorKey.Zero()
			}
			return nil, fmt.Errorf("start P2P: %w", err)
		}

		logger.Info().
			Str("id", p2pNode.ID().String()).
			Int("port", cfg.P2P.Port).
			Bool("discovery", !cfg.P2P.NoDiscover).
			Msg("P2P node started")

		// Heartbeat topic.
		if err := p2pNode.JoinHeartbeat(); err != nil {
			logger.Warn().Err(err).Msg("Failed to join heartbeat topic")
		} else {
			p2pNode.SetHeartbeatHandler(func(msg *p2p.HeartbeatMessage) {
				if poaEngine != nil && !poaEngine.IsValidator(msg.PubKey) {
					return
				}
				tracker.RecordHeartbeat(msg.PubKey)
			})
			logger.Info().Msg("Heartbeat protocol joined")
		}

		// Sync protocol.
		syncer = p2p.NewSyncer(p2pNode)
		syncer.RegisterHandler(func(fromHeight uint64, max uint32) []*block.Block {
			var blocks []*block.Block
			for h := fromHeight; h < fromHeight+uint64(max); h++ {
				blk, err := ch.GetBlockByHeight(h)
				if err != nil {
					break
				}
				blocks = append(blocks, blk)
			}
			return blocks
		})
		syncer.RegisterHeightHandler(func() (uint64, string) {
			return ch.Height(), ch.TipHash().String()
		})
		logger.Info().Msg("Chain sync protocol registered")
	} else {
		logger.Warn().Msg("P2P disabled by config; node will run offline")
	}

	// Stake handler.
	if poa, ok := engine.(*consensus.PoA); ok {
		stakeChecker := consensus.NewUTXOStakeChecker(utxoStore, genesis.Protocol.Consensus.ValidatorStake)

		ch.SetStakeHandler(func(pubKey []byte) {
			poa.AddValidator(pubKey)
			logger.Info().
				Str("pubkey", hex.EncodeToString(pubKey)[:16]+"...").
				Msg("Validator registered via stake")

			if validatorKey != nil && poa.GetSigner() == nil &&
				bytes.Equal(pubKey, validatorKey.PublicKey()) {
				if err := poa.SetSigner(validatorKey); err == nil {
					logger.Info().Msg("Validator key authorized after stake sync")
				}
			}
		})

		ch.SetUnstakeHandler(func(pubKey []byte) {
			ok, _ := stakeChecker.HasStake(pubKey)
			if !ok {
				poa.RemoveValidator(pubKey)
				logger.Info().
					Str("pubkey", hex.EncodeToString(pubKey)[:16]+"...").
					Msg("Validator removed (stake withdrawn)")
			}
		})

		// Recover staked validators on restart.
		if ch.Height() > 0 {
			stakedPKs, err := utxoStore.GetAllStakedValidators()
			if err != nil {
				logger.Warn().Err(err).Msg("Failed to scan staked validators")
			} else {
				for _, pk := range stakedPKs {
					if ok, _ := stakeChecker.HasStake(pk); ok {
						poa.AddValidator(pk)
					}
				}
				if len(stakedPKs) > 0 {
					logger.Info().Int("count", len(stakedPKs)).Msg("Staked validators recovered from UTXO set")
				}
			}
		}

		// Set signer after staked validators are recovered.
		if validatorKey != nil {
			if err := poa.SetSigner(validatorKey); err != nil {
				logger.Warn().Err(err).Msg("Validator key not yet authorized (will activate after stake TX is synced)")
			}
		}
	}

	// Reverted-tx handler.
	ch.SetRevertedTxHandler(func(txs []*tx.Transaction) {
		reinserted := 0
		for _, t := range txs {
			if _, err := pool.Add(t); err == nil {
				reinserted++
			}
		}
		if reinserted > 0 {
			logger.Info().
				Int("reverted", len(txs)).
				Int("reinserted", reinserted).
				Msg("Reverted transactions returned to mempool")
		}
	})

	// ── 11. RPC server ──────────────────────────────────────────────
	var rpcServer *rpc.Server
	if cfg.RPC.Enabled {
		rpcAddr := fmt.Sprintf("%s:%d", cfg.RPC.Addr, cfg.RPC.Port)
		rpcServer = rpc.New(rpcAddr, ch, utxoStore, pool, p2pNode, genesis, engine, cfg.RPC)
		if err := rpcServer.Start(); err != nil {
			if p2pNode != nil {
				p2pNode.Stop()
			}
			db.Close()
			if validatorKey != nil {
				validatorKey.Zero()
			}
			return nil, fmt.Errorf("start RPC at %s: %w", rpcAddr, err)
		}

		// Wire token store.
		rpcServer.SetTokenStore(tokenStore)

		// Wire validator tracker.
		rpcServer.SetValidatorTracker(tracker)

		// Wire ban manager.
		if p2pNode != nil {
			rpcServer.SetBanManager(p2pNode.BanManager)
		}

		logger.Info().Str("addr", rpcServer.Addr()).Msg("RPC server started")

		// Wallet RPC.
		if cfg.Wallet.Enabled {
			ks, ksErr := wallet.NewKeystore(cfg.KeystoreDir())
			if ksErr != nil {
				rpcServer.Stop()
				if p2pNode != nil {
					p2pNode.Stop()
				}
				db.Close()
				if validatorKey != nil {
					validatorKey.Zero()
				}
				return nil, fmt.Errorf("create wallet keystore: %w", ksErr)
			}
			rpcServer.SetKeystore(ks)
			rpcServer.SetWalletTxIndex(rpc.NewWalletTxIndex(db))
			logger.Info().Str("path", cfg.KeystoreDir()).Msg("Wallet RPC enabled")
		}
	} else {
		if cfg.Wallet.Enabled {
			logger.Warn().Msg("wallet.enabled is true but RPC is disabled; wallet RPC endpoints unavailable")
		}
		logger.Warn().Msg("RPC disabled by config")
	}

	ctx, cancel := context.WithCancel(context.Background())

	n := &Node{
		cfg:          cfg,
		genesis:      genesis,
		logger:       logger,
		db:           db,
		utxoStore:    utxoStore,
		engine:       engine,
		ch:           ch,
		pool:         pool,
		tracker:      tracker,
		tokenStore:   tokenStore,
		p2pNode:      p2pNode,
		syncer:       syncer,
		rpcServer:    rpcServer,
		validatorKey: validatorKey,
		poaEngine:    poaEngine,
		scMiners:     make(map[types.ChainID]context.CancelFunc),
		scHBs:        make(map[types.ChainID]context.CancelFunc),
		ctx:          ctx,
		cancel:       cancel,
	}

	// Wire nodeRef for the root chain block handler sync trigger.
	nodeRef = n

	// ── 12. Sub-chain manager ───────────────────────────────────────
	if genesis.Protocol.SubChain.Enabled {
		if err := n.setupSubChains(); err != nil {
			n.Stop()
			return nil, fmt.Errorf("setup sub-chains: %w", err)
		}
	}

	return n, nil
}

// Start launches background goroutines: startup sync, sync loop, miner, heartbeat.
func (n *Node) Start() error {
	// Startup sync.
	if n.p2pNode != nil && n.syncer != nil {
		n.runStartupSync()
		n.wg.Add(1)
		go func() {
			defer n.wg.Done()
			n.runSyncLoop()
		}()
	}

	// Mining.
	if n.cfg.Mining.Enabled {
		coinbaseAddr, err := resolveCoinbase(n.cfg.Mining.Coinbase, n.validatorKey)
		if err != nil {
			return fmt.Errorf("resolve coinbase: %w", err)
		}

		m := miner.New(n.ch, n.engine, n.pool, coinbaseAddr,
			n.genesis.Protocol.Consensus.BlockReward,
			n.genesis.Protocol.Consensus.MaxSupply,
			n.ch.Supply)
		blockTime := time.Duration(n.genesis.Protocol.Consensus.BlockTime) * time.Second

		n.logger.Info().
			Str("coinbase", hex.EncodeToString(coinbaseAddr[:])[:16]+"...").
			Uint64("reward", n.genesis.Protocol.Consensus.BlockReward).
			Dur("interval", blockTime).
			Msg("Block production enabled")

		// Start heartbeat immediately.
		if n.validatorKey != nil {
			n.wg.Add(1)
			go func() {
				defer n.wg.Done()
				n.runHeartbeat(60 * time.Second)
			}()
		}

		// Wait stabilization period then mine.
		n.wg.Add(1)
		go func() {
			defer n.wg.Done()
			stabilize := 3 * blockTime
			n.logger.Info().Dur("delay", stabilize).Msg("Waiting for chain to stabilize before mining")
			select {
			case <-n.ctx.Done():
				return
			case <-time.After(stabilize):
			}
			n.runMiner(m, blockTime)
		}()
	}

	n.logger.Info().
		Uint64("height", n.ch.Height()).
		Str("tip", n.ch.TipHash().String()[:16]+"...").
		Bool("mining", n.cfg.Mining.Enabled).
		Msg("Node started successfully")

	return nil
}

// Stop performs graceful shutdown in reverse order.
func (n *Node) Stop() {
	n.cancel()
	n.wg.Wait()

	if n.rpcServer != nil {
		n.rpcServer.Stop()
	}
	if n.p2pNode != nil {
		n.p2pNode.Stop()
	}
	if n.validatorKey != nil {
		n.validatorKey.Zero()
	}
	if n.db != nil {
		n.db.Close()
	}

	n.logger.Info().Msg("Goodbye!")
}

// RPCAddr returns the address the RPC server is listening on.
func (n *Node) RPCAddr() string {
	if n.rpcServer == nil {
		return ""
	}
	return n.rpcServer.Addr()
}

// Height returns the current chain height.
func (n *Node) Height() uint64 {
	return n.ch.Height()
}

// ── Sync ────────────────────────────────────────────────────────────

func (n *Node) runSyncLoop() {
	if n.p2pNode == nil {
		return
	}
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-n.ctx.Done():
			return
		case <-ticker.C:
			if len(n.p2pNode.PeerList()) == 0 {
				continue
			}
			n.runStartupSync()
		}
	}
}

func (n *Node) runStartupSync() {
	if n.p2pNode == nil || n.syncer == nil {
		return
	}
	peers := n.p2pNode.PeerList()
	if len(peers) == 0 {
		n.logger.Info().Msg("No peers for startup sync")
		return
	}

	var bestPeer peer.ID
	var bestHeight uint64
	var bestTipHash string
	limit := 3
	if len(peers) < limit {
		limit = len(peers)
	}
	localTip := n.ch.TipHash().String()
	for _, p := range peers[:limit] {
		reqCtx, cancel := context.WithTimeout(n.ctx, 5*time.Second)
		resp, err := n.syncer.RequestHeight(reqCtx, p.ID)
		cancel()
		if err != nil {
			continue
		}
		if resp.Height > bestHeight {
			bestHeight = resp.Height
			bestTipHash = resp.TipHash
			bestPeer = p.ID
		} else if resp.Height == bestHeight && resp.TipHash != bestTipHash {
			// Peer at same height with a different tip — track the one
			// that also differs from our local tip for fork detection.
			if resp.TipHash != localTip {
				bestTipHash = resp.TipHash
				bestPeer = p.ID
			}
		}
	}

	localHeight := n.ch.Height()

	// Detect same-height fork: heights match but tips differ.
	if bestHeight == localHeight && bestHeight > 0 {
		if bestTipHash != "" && bestTipHash != localTip {
			n.logger.Info().
				Uint64("height", localHeight).
				Str("local_tip", localTip[:16]+"...").
				Str("peer_tip", bestTipHash[:16]+"...").
				Msg("Same-height fork detected, resolving")
			n.resolveFork(bestPeer, localHeight, bestHeight)
		}
		return
	}

	if bestHeight <= localHeight {
		n.logger.Info().Uint64("height", localHeight).Msg("Chain is up to date")
		return
	}

	total := bestHeight - localHeight
	n.logger.Info().
		Uint64("local", localHeight).
		Uint64("remote", bestHeight).
		Uint64("blocks", total).
		Msg("Syncing chain")

	syncStart := time.Now()

	for from := localHeight + 1; from <= bestHeight; from += 500 {
		max := uint32(500)
		if from+uint64(max)-1 > bestHeight {
			max = uint32(bestHeight - from + 1)
		}

		reqCtx, cancel := context.WithTimeout(n.ctx, 30*time.Second)
		blocks, err := n.syncer.RequestBlocks(reqCtx, bestPeer, from, max)
		cancel()
		if err != nil {
			n.logger.Warn().Err(err).Uint64("from", from).Msg("Sync request failed")
			break
		}

		for _, blk := range blocks {
			if err := n.ch.ProcessBlock(blk); err != nil {
				if errors.Is(err, chain.ErrBlockKnown) {
					continue
				}
				if errors.Is(err, chain.ErrPrevNotFound) {
					n.logger.Info().
						Uint64("height", blk.Header.Height).
						Msg("Fork detected during sync, resolving")
					n.resolveFork(bestPeer, blk.Header.Height, bestHeight)
					return
				}
				n.logger.Warn().Err(err).Uint64("height", blk.Header.Height).Msg("Sync block failed")
				return
			}
			n.pool.RemoveConfirmed(blk.Transactions)
			token.ExtractAndStoreMetadata(n.tokenStore, blk)
		}

		synced := n.ch.Height() - localHeight
		pct := float64(synced) / float64(total) * 100
		elapsed := time.Since(syncStart).Seconds()
		bps := float64(synced) / elapsed
		remaining := ""
		if bps > 0 {
			eta := float64(total-synced) / bps
			remaining = fmt.Sprintf("%.0fs", eta)
		}

		n.logger.Info().
			Uint64("height", n.ch.Height()).
			Uint64("target", bestHeight).
			Str("progress", fmt.Sprintf("%.1f%%", pct)).
			Str("speed", fmt.Sprintf("%.0f blk/s", bps)).
			Str("eta", remaining).
			Msg("Syncing")
	}

	elapsed := time.Since(syncStart)
	n.logger.Info().
		Uint64("height", n.ch.Height()).
		Dur("elapsed", elapsed).
		Msg("Sync complete")
}

func (n *Node) resolveFork(peerID peer.ID, failedHeight, peerTip uint64) {
	searchFrom := failedHeight - 1
	if searchFrom > n.ch.Height() {
		searchFrom = n.ch.Height()
	}

	var ancestorHeight uint64
	found := false

	for h := searchFrom; ; h-- {
		reqCtx, cancel := context.WithTimeout(n.ctx, 5*time.Second)
		peerBlocks, err := n.syncer.RequestBlocks(reqCtx, peerID, h, 1)
		cancel()
		if err != nil || len(peerBlocks) == 0 {
			if h == 0 {
				break
			}
			continue
		}

		localBlk, err := n.ch.GetBlockByHeight(h)
		if err != nil {
			if h == 0 {
				break
			}
			continue
		}

		if peerBlocks[0].Hash() == localBlk.Hash() {
			ancestorHeight = h
			found = true
			break
		}

		if h == 0 {
			break // Reached genesis, prevent uint64 underflow.
		}
	}

	if !found {
		n.logger.Warn().
			Uint64("searched_from", searchFrom).
			Msg("Fork resolution failed: no common ancestor found")
		return
	}

	n.logger.Info().
		Uint64("ancestor", ancestorHeight).
		Uint64("peer_tip", peerTip).
		Uint64("fork_blocks", peerTip-ancestorHeight).
		Msg("Common ancestor found, downloading fork blocks")

	for from := ancestorHeight + 1; from <= peerTip; from += 500 {
		max := uint32(500)
		if from+uint64(max)-1 > peerTip {
			max = uint32(peerTip - from + 1)
		}

		reqCtx, cancel := context.WithTimeout(n.ctx, 30*time.Second)
		blocks, err := n.syncer.RequestBlocks(reqCtx, peerID, from, max)
		cancel()
		if err != nil {
			n.logger.Warn().Err(err).Uint64("from", from).Msg("Fork sync request failed")
			return
		}

		for _, blk := range blocks {
			if err := n.ch.ProcessBlock(blk); err != nil {
				if errors.Is(err, chain.ErrBlockKnown) {
					continue
				}
				n.logger.Warn().Err(err).
					Uint64("height", blk.Header.Height).
					Msg("Fork sync block failed")
				return
			}
			n.pool.RemoveConfirmed(blk.Transactions)
			token.ExtractAndStoreMetadata(n.tokenStore, blk)
		}
	}

	n.logger.Info().
		Uint64("height", n.ch.Height()).
		Str("tip", n.ch.TipHash().String()[:16]+"...").
		Msg("Fork resolved")
}

// ── Mining ──────────────────────────────────────────────────────────

func (n *Node) runMiner(m *miner.Miner, blockTime time.Duration) {
	ticker := time.NewTicker(blockTime)
	defer ticker.Stop()

	for {
		select {
		case <-n.ctx.Done():
			n.logger.Info().Msg("Block production stopped")
			return
		case <-ticker.C:
			nextHeight := n.ch.Height() + 1
			now := uint64(time.Now().Unix())

			// Time-slot-based election: check if we're in-turn.
			if n.poaEngine != nil && !n.poaEngine.IsInTurn(now) {
				// Not in-turn. Identify the expected in-turn validator.
				expectedPub := n.poaEngine.SlotValidator(now)

				// If the in-turn validator is online, don't produce.
				if n.tracker != nil && expectedPub != nil && n.tracker.IsOnline(expectedPub) {
					continue
				}

				// In-turn validator appears offline. Wait staggered backup delay
				// (proportional to our distance from the in-turn slot).
				delay := n.poaEngine.BackupDelay(now)
				n.logger.Debug().
					Uint64("height", nextHeight).
					Dur("backup_delay", delay).
					Msg("Not in-turn, waiting backup delay")

				select {
				case <-n.ctx.Done():
					return
				case <-time.After(delay):
				}

				// Re-check after delay: a block may have arrived.
				if n.ch.Height() >= nextHeight {
					continue
				}

				if expectedPub != nil && n.tracker != nil {
					n.tracker.RecordMiss(expectedPub)
				}
			}

			// Re-check: a block may have arrived via gossip since we read the tip.
			if n.ch.Height() >= nextHeight {
				continue
			}

			blk, err := m.ProduceBlock()
			if err != nil {
				n.logger.Error().Err(err).Msg("Failed to produce block")
				continue
			}

			if err := n.ch.ProcessBlock(blk); err != nil {
				n.logger.Error().Err(err).Msg("Failed to process own block")
				if errors.Is(err, chain.ErrCoinbaseNotMature) {
					for _, t := range blk.Transactions[1:] {
						n.pool.Remove(t.Hash())
					}
					n.logger.Info().Msg("Evicted mempool transactions due to coinbase maturity")
				}
				continue
			}
			n.pool.RemoveConfirmed(blk.Transactions)

			if n.poaEngine != nil && n.tracker != nil {
				if signer := n.poaEngine.IdentifySigner(blk.Header); signer != nil {
					n.tracker.RecordBlock(signer)
				}
			}

			if n.p2pNode != nil {
				if err := n.p2pNode.BroadcastBlock(blk); err != nil {
					n.logger.Error().Err(err).Msg("Failed to broadcast block")
				}
			}

			n.logger.Info().
				Uint64("height", blk.Header.Height).
				Str("hash", blk.Hash().String()[:16]+"...").
				Int("txs", len(blk.Transactions)).
				Uint64("reward", blk.Transactions[0].Outputs[0].Value).
				Msg("Block produced")
		}
	}
}

// ── Heartbeat ───────────────────────────────────────────────────────

func (n *Node) runHeartbeat(interval time.Duration) {
	if n.p2pNode == nil {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	pubKey := n.validatorKey.PublicKey()
	n.logger.Info().Dur("interval", interval).Msg("Heartbeat broadcast started")

	n.sendHeartbeat(pubKey)

	for {
		select {
		case <-n.ctx.Done():
			n.logger.Info().Msg("Heartbeat broadcast stopped")
			return
		case <-ticker.C:
			n.sendHeartbeat(pubKey)
		}
	}
}

func (n *Node) sendHeartbeat(pubKey []byte) {
	ts := time.Now().Unix()
	height := n.ch.Height()

	data := p2p.HeartbeatSigningBytes(pubKey, height, ts)
	hash := crypto.Hash(data)
	sig, err := n.validatorKey.Sign(hash[:])
	if err != nil {
		n.logger.Error().Err(err).Msg("Failed to sign heartbeat")
		return
	}

	msg := &p2p.HeartbeatMessage{
		PubKey:    pubKey,
		Height:    height,
		Timestamp: ts,
		Signature: sig,
	}

	if err := n.p2pNode.BroadcastHeartbeat(msg); err != nil {
		n.logger.Debug().Err(err).Msg("Failed to broadcast heartbeat")
	}
}

// ── Sub-chains ──────────────────────────────────────────────────────

func (n *Node) setupSubChains() error {
	syncFilter := subchain.NewSyncFilter(n.cfg.SubChainSync)
	scManager, err := subchain.NewManager(subchain.ManagerConfig{
		ParentDB:   n.db,
		ParentID:   types.ChainID{},
		Rules:      &n.genesis.Protocol.SubChain,
		SyncFilter: syncFilter,
	})
	if err != nil {
		return fmt.Errorf("create sub-chain manager: %w", err)
	}
	n.scManager = scManager

	// Build mine filter.
	var mineFilter *subchain.MineFilter
	if len(n.cfg.SubChainMineIDs) > 0 {
		if len(n.cfg.SubChainMineIDs) > config.MaxSubChainMiners {
			return fmt.Errorf("too many sub-chain mine IDs: %d (max %d)", len(n.cfg.SubChainMineIDs), config.MaxSubChainMiners)
		}
		mineFilter = subchain.NewMineFilter(n.cfg.SubChainMineIDs)
		n.logger.Info().
			Int("count", len(n.cfg.SubChainMineIDs)).
			Msg("Sub-chain mining filter configured")
	}

	// Resolve coinbase for sub-chain mining.
	var scCoinbase types.Address
	if n.cfg.Mining.Coinbase != "" || n.validatorKey != nil {
		scCoinbase, _ = resolveCoinbase(n.cfg.Mining.Coinbase, n.validatorKey)
	}

	// Spawn handler.
	scManager.SetSpawnHandler(func(chainID types.ChainID, sr *subchain.SpawnResult) {
		n.handleSubChainSpawn(chainID, sr, mineFilter, scCoinbase)
	})

	// Stop handler.
	scManager.SetStopHandler(func(chainID types.ChainID) {
		n.handleSubChainStop(chainID)
	})

	// Restore previously registered sub-chains.
	if err := scManager.RestoreChains(); err != nil {
		n.logger.Warn().Err(err).Msg("Failed to restore sub-chains")
	} else if scManager.Count() > 0 {
		n.logger.Info().
			Int("registered", scManager.Count()).
			Int("syncing", scManager.SyncedCount()).
			Str("sync_mode", string(n.cfg.SubChainSync.Mode)).
			Msg("Sub-chains restored")
	}

	// Registration handler.
	n.ch.SetRegistrationHandler(func(txHash types.Hash, idx uint32, value uint64, data []byte, height uint64) {
		if err := scManager.HandleRegistration(txHash, idx, value, data, height); err != nil {
			n.logger.Warn().Err(err).
				Str("tx", txHash.String()[:16]+"...").
				Uint32("index", idx).
				Msg("Sub-chain registration failed")
		} else {
			chainID := subchain.DeriveChainID(txHash, idx)
			n.logger.Info().
				Str("chain_id", chainID.String()[:16]+"...").
				Uint64("height", height).
				Msg("Sub-chain registered")
		}
	})

	// Deregistration handler.
	n.ch.SetDeregistrationHandler(func(txHash types.Hash, idx uint32) {
		chainID := subchain.DeriveChainID(txHash, idx)
		if err := scManager.HandleDeregistration(txHash, idx); err != nil {
			n.logger.Warn().Err(err).
				Str("chain_id", chainID.String()[:16]+"...").
				Msg("Sub-chain deregistration failed")
		} else {
			n.logger.Info().
				Str("chain_id", chainID.String()[:16]+"...").
				Msg("Sub-chain deregistered (reorg)")
		}
	})

	if n.rpcServer != nil {
		n.rpcServer.SetSubChainManager(scManager)
	}
	n.logger.Info().Msg("Sub-chain system enabled")
	return nil
}

func (n *Node) handleSubChainSpawn(chainID types.ChainID, sr *subchain.SpawnResult,
	mineFilter *subchain.MineFilter, scCoinbase types.Address) {

	idHex := hex.EncodeToString(chainID[:])
	scLog := n.logger.With().Str("subchain", idHex[:8]).Logger()

	// Wire PoW DifficultyFn.
	if pow, ok := sr.Engine.(*consensus.PoW); ok && pow.AdjustInterval > 0 {
		pow.DifficultyFn = func(height uint64) uint64 {
			if height <= 1 {
				return pow.InitialDifficulty
			}
			prevBlk, err := sr.Chain.GetBlockByHeight(height - 1)
			if err != nil {
				return pow.InitialDifficulty
			}
			return pow.ExpectedDifficulty(height, prevBlk.Header.Difficulty, func(h uint64) (uint64, error) {
				b, e := sr.Chain.GetBlockByHeight(h)
				if e != nil {
					return 0, e
				}
				return b.Header.Timestamp, nil
			})
		}
		scLog.Info().Int("interval", pow.AdjustInterval).Msg("PoW difficulty adjustment enabled")
	}

	// Wire dynamic validator staking for PoA sub-chains.
	if poaEng, ok := sr.Engine.(*consensus.PoA); ok && sr.Genesis.Protocol.Consensus.ValidatorStake > 0 {
		minStake := sr.Genesis.Protocol.Consensus.ValidatorStake
		stakeChecker := consensus.NewUTXOStakeChecker(sr.UTXOs, minStake)

		sr.Chain.SetStakeHandler(func(pubKey []byte) {
			poaEng.AddValidator(pubKey)
			scLog.Info().
				Str("pubkey", hex.EncodeToString(pubKey[:8])+"...").
				Msg("Dynamic validator added via stake")
		})
		sr.Chain.SetUnstakeHandler(func(pubKey []byte) {
			ok, _ := stakeChecker.HasStake(pubKey)
			if !ok {
				poaEng.RemoveValidator(pubKey)
				scLog.Info().
					Str("pubkey", hex.EncodeToString(pubKey[:8])+"...").
					Msg("Dynamic validator removed (unstaked)")
			}
		})

		if sr.Chain.Height() > 0 {
			stakedPKs, err := sr.UTXOs.GetAllStakedValidators()
			if err == nil {
				for _, pk := range stakedPKs {
					if ok, _ := stakeChecker.HasStake(pk); ok {
						poaEng.AddValidator(pk)
					}
				}
				if len(stakedPKs) > 0 {
					scLog.Info().Int("count", len(stakedPKs)).Msg("Recovered staked validators")
				}
			}
		}
	}

	// Per-chain validator tracker for PoA sub-chains.
	var scTracker *consensus.ValidatorTracker
	var scPoA *consensus.PoA
	if poa, ok := sr.Engine.(*consensus.PoA); ok {
		scPoA = poa
		scTracker = consensus.NewValidatorTracker(60 * time.Second)

		if n.p2pNode != nil {
			if err := n.p2pNode.JoinSubChainHeartbeat(idHex); err != nil {
				scLog.Warn().Err(err).Msg("Failed to join sub-chain heartbeat topic")
			} else {
				scPoALocal := scPoA
				n.p2pNode.SetSubChainHeartbeatHandler(idHex, func(msg *p2p.HeartbeatMessage) {
					if scPoALocal != nil && !scPoALocal.IsValidator(msg.PubKey) {
						return
					}
					scTracker.RecordHeartbeat(msg.PubKey)
				})
				scLog.Info().Msg("Sub-chain heartbeat joined")
			}
		}

		if n.rpcServer != nil {
			n.rpcServer.SetSubChainTracker(idHex, scTracker)
		}
	}

	if n.p2pNode != nil && n.syncer != nil {
		// Join P2P topics.
		if err := n.p2pNode.JoinSubChain(idHex); err != nil {
			scLog.Warn().Err(err).Msg("Failed to join sub-chain P2P topics")
		} else {
			scLog.Info().Msg("Joined sub-chain P2P topics")
		}

		// Sync handlers.
		n.syncer.RegisterSubChainHandler(idHex, func(from uint64, max uint32) []*block.Block {
			var blocks []*block.Block
			for h := from; h < from+uint64(max); h++ {
				blk, err := sr.Chain.GetBlockByHeight(h)
				if err != nil {
					break
				}
				blocks = append(blocks, blk)
			}
			return blocks
		})
		n.syncer.RegisterSubChainHeightHandler(idHex, func() (uint64, string) {
			return sr.Chain.Height(), sr.Chain.TipHash().String()
		})

		// Block handler with sync trigger.
		var syncing atomic.Bool
		n.p2pNode.SetSubChainBlockHandler(idHex, func(from peer.ID, data []byte) {
			var blk block.Block
			if err := json.Unmarshal(data, &blk); err != nil {
				n.p2pNode.BanManager.RecordOffense(from, p2p.PenaltyInvalidBlock, "sc unmarshal: "+err.Error())
				return
			}
			if err := sr.Chain.ProcessBlock(&blk); err != nil {
				if errors.Is(err, chain.ErrPrevNotFound) && syncing.CompareAndSwap(false, true) {
					go func() {
						defer syncing.Store(false)
						n.runSubChainSync(sr.Chain, sr.Pool, idHex, scLog)
					}()
				} else if !errors.Is(err, chain.ErrBlockKnown) &&
					!errors.Is(err, chain.ErrPrevNotFound) &&
					!errors.Is(err, chain.ErrForkDetected) {
					n.p2pNode.BanManager.RecordOffense(from, p2p.PenaltyInvalidBlock, err.Error())
				}
				return
			}
			sr.Pool.RemoveConfirmed(blk.Transactions)

			if scPoA != nil && scTracker != nil {
				if signer := scPoA.IdentifySigner(blk.Header); signer != nil {
					scTracker.RecordBlock(signer)
				}
			}

			scLog.Info().
				Uint64("height", blk.Header.Height).
				Str("hash", blk.Hash().String()[:16]+"...").
				Msg("Sub-chain block received")
		})

		// Tx handler.
		n.p2pNode.SetSubChainTxHandler(idHex, func(from peer.ID, data []byte) {
			var t tx.Transaction
			if err := json.Unmarshal(data, &t); err != nil {
				n.p2pNode.BanManager.RecordOffense(from, p2p.PenaltyInvalidTx, "sc tx unmarshal: "+err.Error())
				return
			}
			if _, err := sr.Pool.Add(&t); err != nil {
				n.p2pNode.BanManager.RecordOffense(from, p2p.PenaltyInvalidTx, err.Error())
				return
			}
		})

		// Initial sync.
		go n.runSubChainSync(sr.Chain, sr.Pool, idHex, scLog)
	}

	// Start PoW miner if listed.
	if !isPoW(sr.Engine) && mineFilter != nil && mineFilter.ShouldMine(chainID) {
		scLog.Warn().Msg("subchain.mine includes a PoA chain ID; ignored (PoW only)")
	}
	if isPoW(sr.Engine) && mineFilter != nil && mineFilter.ShouldMine(chainID) && scCoinbase != (types.Address{}) {
		n.startSubChainMiner(chainID, sr, scCoinbase)
	}

	// Start PoA miner.
	if scPoA != nil && n.validatorKey != nil && n.cfg.Mining.Enabled {
		if err := scPoA.SetSigner(n.validatorKey); err == nil {
			coinbase, _ := resolveCoinbase(n.cfg.Mining.Coinbase, n.validatorKey)
			n.startSubChainPoAMiner(chainID, sr, scPoA, coinbase, scTracker)
		}
	}

	// Start heartbeat for PoA sub-chains.
	if scPoA != nil && n.validatorKey != nil && scPoA.IsValidator(n.validatorKey.PublicKey()) {
		n.startSubChainHeartbeat(chainID, sr.Chain)
	}
}

func (n *Node) handleSubChainStop(chainID types.ChainID) {
	idHex := hex.EncodeToString(chainID[:])
	n.stopSubChainMiner(chainID)
	n.stopSubChainHeartbeat(chainID)
	if n.syncer != nil {
		n.syncer.RemoveSubChainHandler(idHex)
	}
	if n.p2pNode != nil {
		n.p2pNode.LeaveSubChainHeartbeat(idHex)
		n.p2pNode.LeaveSubChain(idHex)
	}
	if n.rpcServer != nil {
		n.rpcServer.RemoveSubChainTracker(idHex)
	}
	n.logger.Info().Str("chain", idHex[:16]+"...").Msg("Left sub-chain P2P topics")
}

// ── Sub-chain sync ──────────────────────────────────────────────────

func (n *Node) runSubChainSync(ch *chain.Chain, pool *mempool.Pool, chainIDHex string, logger zerolog.Logger) {
	if n.p2pNode == nil || n.syncer == nil {
		return
	}
	peers := n.p2pNode.PeerList()
	if len(peers) == 0 {
		return
	}

	var bestPeer peer.ID
	var bestHeight uint64
	var bestTipHash string
	limit := 3
	if len(peers) < limit {
		limit = len(peers)
	}
	localTip := ch.TipHash().String()
	for _, p := range peers[:limit] {
		reqCtx, cancel := context.WithTimeout(n.ctx, 5*time.Second)
		resp, err := n.syncer.RequestSubChainHeight(reqCtx, p.ID, chainIDHex)
		cancel()
		if err != nil {
			continue
		}
		if resp.Height > bestHeight {
			bestHeight = resp.Height
			bestTipHash = resp.TipHash
			bestPeer = p.ID
		} else if resp.Height == bestHeight && resp.TipHash != bestTipHash {
			if resp.TipHash != localTip {
				bestTipHash = resp.TipHash
				bestPeer = p.ID
			}
		}
	}

	localHeight := ch.Height()

	// Detect same-height fork: heights match but tips differ.
	if bestHeight == localHeight && bestHeight > 0 {
		if bestTipHash != "" && bestTipHash != localTip {
			logger.Info().
				Uint64("height", localHeight).
				Msg("Same-height sub-chain fork detected, resolving")
			n.resolveSubChainFork(ch, pool, bestPeer, chainIDHex, localHeight, bestHeight, logger)
		}
		return
	}

	if bestHeight <= localHeight {
		return
	}

	total := bestHeight - localHeight
	logger.Info().
		Uint64("local", localHeight).
		Uint64("remote", bestHeight).
		Uint64("blocks", total).
		Msg("Syncing sub-chain")

	syncStart := time.Now()

	for from := localHeight + 1; from <= bestHeight; from += 500 {
		max := uint32(500)
		if from+uint64(max)-1 > bestHeight {
			max = uint32(bestHeight - from + 1)
		}

		reqCtx, cancel := context.WithTimeout(n.ctx, 30*time.Second)
		blocks, err := n.syncer.RequestSubChainBlocks(reqCtx, bestPeer, chainIDHex, from, max)
		cancel()
		if err != nil {
			logger.Warn().Err(err).Uint64("from", from).Msg("Sub-chain sync request failed")
			break
		}

		for _, blk := range blocks {
			if err := ch.ProcessBlock(blk); err != nil {
				if errors.Is(err, chain.ErrBlockKnown) {
					continue
				}
				if errors.Is(err, chain.ErrPrevNotFound) {
					logger.Info().
						Uint64("height", blk.Header.Height).
						Msg("Fork detected during sub-chain sync, resolving")
					n.resolveSubChainFork(ch, pool, bestPeer, chainIDHex, blk.Header.Height, bestHeight, logger)
					return
				}
				logger.Warn().Err(err).Uint64("height", blk.Header.Height).Msg("Sub-chain sync block failed")
				return
			}
			pool.RemoveConfirmed(blk.Transactions)
		}

		synced := ch.Height() - localHeight
		pct := float64(synced) / float64(total) * 100
		elapsed := time.Since(syncStart).Seconds()
		bps := float64(synced) / elapsed
		remaining := ""
		if bps > 0 {
			eta := float64(total-synced) / bps
			remaining = fmt.Sprintf("%.0fs", eta)
		}

		logger.Info().
			Uint64("height", ch.Height()).
			Uint64("target", bestHeight).
			Str("progress", fmt.Sprintf("%.1f%%", pct)).
			Str("speed", fmt.Sprintf("%.0f blk/s", bps)).
			Str("eta", remaining).
			Msg("Syncing")
	}

	elapsed := time.Since(syncStart)
	logger.Info().
		Uint64("height", ch.Height()).
		Dur("elapsed", elapsed).
		Msg("Sub-chain sync complete")
}

func (n *Node) resolveSubChainFork(ch *chain.Chain, pool *mempool.Pool,
	peerID peer.ID, chainIDHex string, failedHeight, peerTip uint64, logger zerolog.Logger) {

	searchFrom := failedHeight - 1
	if searchFrom > ch.Height() {
		searchFrom = ch.Height()
	}

	var ancestorHeight uint64
	found := false

	for h := searchFrom; ; h-- {
		reqCtx, cancel := context.WithTimeout(n.ctx, 5*time.Second)
		peerBlocks, err := n.syncer.RequestSubChainBlocks(reqCtx, peerID, chainIDHex, h, 1)
		cancel()
		if err != nil || len(peerBlocks) == 0 {
			if h == 0 {
				break
			}
			continue
		}

		localBlk, err := ch.GetBlockByHeight(h)
		if err != nil {
			if h == 0 {
				break
			}
			continue
		}

		if peerBlocks[0].Hash() == localBlk.Hash() {
			ancestorHeight = h
			found = true
			break
		}

		if h == 0 {
			break // Reached genesis, prevent uint64 underflow.
		}
	}

	if !found {
		logger.Warn().
			Uint64("searched_from", searchFrom).
			Msg("Sub-chain fork resolution failed: no common ancestor found")
		return
	}

	logger.Info().
		Uint64("ancestor", ancestorHeight).
		Uint64("peer_tip", peerTip).
		Uint64("fork_blocks", peerTip-ancestorHeight).
		Msg("Sub-chain common ancestor found, downloading fork blocks")

	for from := ancestorHeight + 1; from <= peerTip; from += 500 {
		max := uint32(500)
		if from+uint64(max)-1 > peerTip {
			max = uint32(peerTip - from + 1)
		}

		reqCtx, cancel := context.WithTimeout(n.ctx, 30*time.Second)
		blocks, err := n.syncer.RequestSubChainBlocks(reqCtx, peerID, chainIDHex, from, max)
		cancel()
		if err != nil {
			logger.Warn().Err(err).Uint64("from", from).Msg("Sub-chain fork sync request failed")
			return
		}

		for _, blk := range blocks {
			if err := ch.ProcessBlock(blk); err != nil {
				if errors.Is(err, chain.ErrBlockKnown) {
					continue
				}
				logger.Warn().Err(err).
					Uint64("height", blk.Header.Height).
					Msg("Sub-chain fork sync block failed")
				return
			}
			pool.RemoveConfirmed(blk.Transactions)
		}
	}

	logger.Info().
		Uint64("height", ch.Height()).
		Str("tip", ch.TipHash().String()[:16]+"...").
		Msg("Sub-chain fork resolved")
}

// ── Sub-chain mining ────────────────────────────────────────────────

func (n *Node) startSubChainMiner(chainID types.ChainID,
	sr *subchain.SpawnResult, coinbase types.Address) {

	n.scMinerMu.Lock()
	if _, already := n.scMiners[chainID]; already {
		n.scMinerMu.Unlock()
		return
	}
	mCtx, cancel := context.WithCancel(n.ctx)
	n.scMiners[chainID] = cancel
	n.scMinerMu.Unlock()

	m := miner.New(sr.Chain, sr.Engine, sr.Pool, coinbase,
		sr.Genesis.Protocol.Consensus.BlockReward,
		sr.Genesis.Protocol.Consensus.MaxSupply,
		sr.Chain.Supply)

	idHex := hex.EncodeToString(chainID[:])
	blockTime := time.Duration(sr.Genesis.Protocol.Consensus.BlockTime) * time.Second
	subLogger := n.logger.With().Str("subchain", idHex[:8]).Logger()

	subLogger.Info().Msg("Starting PoW miner for sub-chain")
	go n.runSubChainMiner(mCtx, m, sr.Chain, sr.Pool, idHex, blockTime, subLogger)
}

func (n *Node) stopSubChainMiner(chainID types.ChainID) {
	n.scMinerMu.Lock()
	if cancel, ok := n.scMiners[chainID]; ok {
		cancel()
		delete(n.scMiners, chainID)
	}
	n.scMinerMu.Unlock()
}

func (n *Node) startSubChainPoAMiner(chainID types.ChainID,
	sr *subchain.SpawnResult, poaEng *consensus.PoA, coinbase types.Address,
	tracker *consensus.ValidatorTracker) {

	n.scMinerMu.Lock()
	if _, already := n.scMiners[chainID]; already {
		n.scMinerMu.Unlock()
		return
	}
	mCtx, cancel := context.WithCancel(n.ctx)
	n.scMiners[chainID] = cancel
	n.scMinerMu.Unlock()

	m := miner.New(sr.Chain, sr.Engine, sr.Pool, coinbase,
		sr.Genesis.Protocol.Consensus.BlockReward,
		sr.Genesis.Protocol.Consensus.MaxSupply,
		sr.Chain.Supply)

	idHex := hex.EncodeToString(chainID[:])
	blockTime := time.Duration(sr.Genesis.Protocol.Consensus.BlockTime) * time.Second
	subLogger := n.logger.With().Str("subchain", idHex[:8]).Logger()

	subLogger.Info().Msg("Starting PoA miner for sub-chain")
	go n.runSubChainPoAMiner(mCtx, m, sr.Chain, sr.Pool, idHex, blockTime, poaEng, tracker, subLogger)
}

func (n *Node) runSubChainPoAMiner(ctx context.Context, m *miner.Miner, ch *chain.Chain,
	pool *mempool.Pool, chainIDHex string, blockTime time.Duration,
	poaEngine *consensus.PoA, tracker *consensus.ValidatorTracker, logger zerolog.Logger) {

	ticker := time.NewTicker(blockTime)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info().Msg("Sub-chain PoA miner stopped")
			return
		case <-ticker.C:
			nextHeight := ch.Height() + 1
			now := uint64(time.Now().Unix())

			// Time-slot-based election: check if we're in-turn.
			if !poaEngine.IsInTurn(now) {
				expectedPub := poaEngine.SlotValidator(now)

				// If the in-turn validator is online, don't produce.
				if tracker != nil && expectedPub != nil && tracker.IsOnline(expectedPub) {
					continue
				}

				// In-turn validator appears offline. Wait staggered backup delay.
				delay := poaEngine.BackupDelay(now)
				select {
				case <-ctx.Done():
					return
				case <-time.After(delay):
				}

				if ch.Height() >= nextHeight {
					continue
				}

				if expectedPub != nil && tracker != nil {
					tracker.RecordMiss(expectedPub)
				}
			}

			// Re-check: a block may have arrived via gossip since we read the tip.
			if ch.Height() >= nextHeight {
				continue
			}

			blk, err := m.ProduceBlock()
			if err != nil {
				logger.Warn().Err(err).Msg("Block production failed")
				continue
			}
			if err := ch.ProcessBlock(blk); err != nil {
				logger.Warn().Err(err).Msg("Block rejected")
				if errors.Is(err, chain.ErrCoinbaseNotMature) {
					for _, t := range blk.Transactions[1:] {
						pool.Remove(t.Hash())
					}
					logger.Info().Msg("Evicted mempool transactions due to coinbase maturity")
				}
				continue
			}
			pool.RemoveConfirmed(blk.Transactions)

			if tracker != nil {
				if signer := poaEngine.IdentifySigner(blk.Header); signer != nil {
					tracker.RecordBlock(signer)
				}
			}

			if n.p2pNode != nil {
				if err := n.p2pNode.BroadcastSubChainBlock(chainIDHex, blk); err != nil {
					logger.Warn().Err(err).Msg("Failed to broadcast sub-chain block")
				}
			}

			logger.Info().
				Uint64("height", blk.Header.Height).
				Str("hash", blk.Hash().String()[:16]+"...").
				Msg("Sub-chain block produced")
		}
	}
}

func (n *Node) runSubChainMiner(ctx context.Context, m *miner.Miner, ch *chain.Chain,
	pool *mempool.Pool, chainIDHex string, blockTime time.Duration, logger zerolog.Logger) {

	ticker := time.NewTicker(blockTime)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info().Msg("Sub-chain miner stopped")
			return
		case <-ticker.C:
			blk, err := m.ProduceBlock()
			if err != nil {
				logger.Warn().Err(err).Msg("Block production failed")
				continue
			}
			if err := ch.ProcessBlock(blk); err != nil {
				logger.Warn().Err(err).Msg("Block rejected")
				continue
			}
			pool.RemoveConfirmed(blk.Transactions)

			if n.p2pNode != nil {
				if err := n.p2pNode.BroadcastSubChainBlock(chainIDHex, blk); err != nil {
					logger.Warn().Err(err).Msg("Failed to broadcast sub-chain block")
				}
			}

			logger.Info().
				Uint64("height", blk.Header.Height).
				Str("hash", blk.Hash().String()[:16]+"...").
				Msg("Sub-chain block mined")
		}
	}
}

// ── Sub-chain heartbeat ─────────────────────────────────────────────

func (n *Node) startSubChainHeartbeat(chainID types.ChainID, ch *chain.Chain) {
	n.scHBMu.Lock()
	if _, already := n.scHBs[chainID]; already {
		n.scHBMu.Unlock()
		return
	}
	hCtx, cancel := context.WithCancel(n.ctx)
	n.scHBs[chainID] = cancel
	n.scHBMu.Unlock()

	idHex := hex.EncodeToString(chainID[:])
	subLogger := n.logger.With().Str("subchain", idHex[:8]).Logger()

	go n.runSubChainHeartbeat(hCtx, ch, idHex, 60*time.Second, subLogger)
}

func (n *Node) stopSubChainHeartbeat(chainID types.ChainID) {
	n.scHBMu.Lock()
	if cancel, ok := n.scHBs[chainID]; ok {
		cancel()
		delete(n.scHBs, chainID)
	}
	n.scHBMu.Unlock()
}

func (n *Node) runSubChainHeartbeat(ctx context.Context, ch *chain.Chain,
	chainIDHex string, interval time.Duration, logger zerolog.Logger) {

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	pubKey := n.validatorKey.PublicKey()
	logger.Info().Dur("interval", interval).Msg("Sub-chain heartbeat started")

	n.sendSubChainHeartbeat(pubKey, ch, chainIDHex, logger)

	for {
		select {
		case <-ctx.Done():
			logger.Info().Msg("Sub-chain heartbeat stopped")
			return
		case <-ticker.C:
			n.sendSubChainHeartbeat(pubKey, ch, chainIDHex, logger)
		}
	}
}

func (n *Node) sendSubChainHeartbeat(pubKey []byte, ch *chain.Chain, chainIDHex string, logger zerolog.Logger) {
	if n.p2pNode == nil {
		return
	}
	ts := time.Now().Unix()
	height := ch.Height()

	data := p2p.HeartbeatSigningBytes(pubKey, height, ts)
	hash := crypto.Hash(data)
	sig, err := n.validatorKey.Sign(hash[:])
	if err != nil {
		logger.Error().Err(err).Msg("Failed to sign sub-chain heartbeat")
		return
	}

	msg := &p2p.HeartbeatMessage{
		PubKey:    pubKey,
		Height:    height,
		Timestamp: ts,
		Signature: sig,
	}

	if err := n.p2pNode.BroadcastSubChainHeartbeat(chainIDHex, msg); err != nil {
		logger.Debug().Err(err).Msg("Failed to broadcast sub-chain heartbeat")
	}
}
