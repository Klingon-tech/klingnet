// Klingnet full node daemon.
//
// Usage:
//
//	klingnetd [--mine --validator-key=...] Run node
//	klingnetd --help                       Show help
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
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

func main() {
	// ── 1. Load config (defaults → file → flags) ────────────────────────
	cfg, flags, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// ── 1a. Set address HRP based on network ────────────────────────
	if cfg.Network == config.Testnet {
		types.SetAddressHRP(types.TestnetHRP)
	} else {
		types.SetAddressHRP(types.MainnetHRP)
	}

	// ── 2. Init logger ──────────────────────────────────────────────────
	// Default to logging to <datadir>/logs/klingnet.log alongside console.
	logFile := cfg.Log.File
	if logFile == "" {
		logsDir := cfg.LogsDir()
		if err := os.MkdirAll(logsDir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "Error creating logs dir: %v\n", err)
			os.Exit(1)
		}
		logFile = logsDir + "/klingnet.log"
	}
	if err := klog.Init(cfg.Log.Level, cfg.Log.JSON, logFile); err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing logger: %v\n", err)
		os.Exit(1)
	}
	logger := klog.WithComponent("node")

	// ── 3. Genesis (hardcoded, not loaded from file) ────────────────────
	genesis := config.GenesisFor(cfg.Network)

	logger.Info().
		Str("chain_id", genesis.ChainID).
		Str("network", string(cfg.Network)).
		Str("consensus", genesis.Protocol.Consensus.Type).
		Int("block_time", genesis.Protocol.Consensus.BlockTime).
		Msg("Starting Klingnet Chain Node")

	// ── 4. Open storage ─────────────────────────────────────────────────
	db, err := storage.NewBadger(cfg.ChainDataDir())
	if err != nil {
		logger.Fatal().Err(err).Str("path", cfg.ChainDataDir()).Msg("Failed to open database")
	}
	defer db.Close()

	utxoStore := utxo.NewStore(db)

	logger.Info().Str("path", cfg.ChainDataDir()).Msg("Database opened")

	// ── 5. Create consensus engine ──────────────────────────────────────
	// Load validator key if provided (needed for root mining AND sub-chain mining).
	var validatorKey *crypto.PrivateKey
	if flags.ValidatorKey != "" {
		validatorKey, err = loadValidatorKey(flags.ValidatorKey)
		if err != nil {
			logger.Fatal().Err(err).Str("path", flags.ValidatorKey).Msg("Failed to load validator key")
		}
		defer validatorKey.Zero()
		logger.Info().
			Str("pubkey", hex.EncodeToString(validatorKey.PublicKey())[:16]+"...").
			Msg("Validator key loaded")
	}
	if flags.Mine && validatorKey == nil {
		logger.Fatal().Msg("--mine requires --validator-key")
	}

	engine, err := createEngine(genesis, nil) // Signer set after staked validators are recovered.
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to create consensus engine")
	}

	// Wire stake checker if validator staking is configured.
	if genesis.Protocol.Consensus.ValidatorStake > 0 {
		if poa, ok := engine.(*consensus.PoA); ok {
			sc := consensus.NewUTXOStakeChecker(utxoStore, genesis.Protocol.Consensus.ValidatorStake)
			poa.SetStakeChecker(sc)
			logger.Info().
				Uint64("min_stake", genesis.Protocol.Consensus.ValidatorStake).
				Msg("Validator staking enabled")
		}
	}

	// ── 6. Create chain (auto-recovers tip from DB) ─────────────────────
	ch, err := chain.New(types.ChainID{}, db, utxoStore, engine)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to create chain")
	}
	ch.SetConsensusRules(genesis.Protocol.Consensus)

	// Init from genesis if this is a fresh database.
	state := ch.State()
	if state.IsGenesis() {
		if err := ch.InitFromGenesis(genesis); err != nil {
			logger.Fatal().Err(err).Msg("Failed to initialize from genesis")
		}
		logger.Info().Msg("Chain initialized from genesis")
	} else {
		logger.Info().
			Uint64("height", ch.Height()).
			Str("tip", ch.TipHash().String()[:16]+"...").
			Msg("Chain resumed from database")
	}

	// ── 7. Create mempool ───────────────────────────────────────────────
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

	// ── 7a. Validator tracker (in-memory liveness stats) ─────────────────
	tracker := consensus.NewValidatorTracker(60 * time.Second)

	// Resolve PoA engine pointer for block signer identification.
	var poaEngine *consensus.PoA
	if poa, ok := engine.(*consensus.PoA); ok {
		poaEngine = poa
	}

	// ── 8. Create P2P node ──────────────────────────────────────────────
	p2pNode := p2p.New(p2p.Config{
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

	// Wire handshake: verify peers are on the same chain.
	genesisHash, _ := genesis.Hash()
	p2pNode.SetGenesisHash(genesisHash)
	p2pNode.SetHeightFn(func() uint64 { return ch.Height() })

	// Wire block handler: gossip → process → mempool cleanup.
	p2pNode.SetBlockHandler(func(from peer.ID, data []byte) {
		var blk block.Block
		if err := json.Unmarshal(data, &blk); err != nil {
			logger.Debug().Err(err).Msg("Failed to unmarshal block")
			p2pNode.BanManager.RecordOffense(from, p2p.PenaltyInvalidBlock, "unmarshal: "+err.Error())
			return
		}
		if err := ch.ProcessBlock(&blk); err != nil {
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

		// Track block signer for validator status.
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

	// Wire tx handler: gossip → mempool add.
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
		logger.Fatal().Err(err).Msg("Failed to start P2P")
	}
	defer p2pNode.Stop()

	logger.Info().
		Str("id", p2pNode.ID().String()).
		Int("port", cfg.P2P.Port).
		Bool("discovery", !cfg.P2P.NoDiscover).
		Msg("P2P node started")

	// ── 8+. Join heartbeat topic ──────────────────────────────────────
	if err := p2pNode.JoinHeartbeat(); err != nil {
		logger.Warn().Err(err).Msg("Failed to join heartbeat topic")
	} else {
		p2pNode.SetHeartbeatHandler(func(msg *p2p.HeartbeatMessage) {
			// Only record heartbeats from known validators.
			if poaEngine != nil && !poaEngine.IsValidator(msg.PubKey) {
				return
			}
			tracker.RecordHeartbeat(msg.PubKey)
		})
		logger.Info().Msg("Heartbeat protocol joined")
	}

	// ── 8a. Wire chain sync protocol ──────────────────────────────────
	syncer := p2p.NewSyncer(p2pNode)
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

	// ── 8b. Wire stake handler ────────────────────────────────────────
	if poa, ok := engine.(*consensus.PoA); ok {
		stakeChecker := consensus.NewUTXOStakeChecker(utxoStore, genesis.Protocol.Consensus.ValidatorStake)

		ch.SetStakeHandler(func(pubKey []byte) {
			poa.AddValidator(pubKey)
			logger.Info().
				Str("pubkey", hex.EncodeToString(pubKey)[:16]+"...").
				Msg("Validator registered via stake")

			// If our validator key was pending (not yet authorized at startup),
			// try to set the signer now that this validator has been added.
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

		// Recover staked validators from the UTXO set on restart.
		// Without this, non-genesis validators are lost when the node restarts.
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

		// Set signer AFTER staked validators are recovered so non-genesis
		// validators (registered via staking) are accepted.
		// If the key is not yet authorized (e.g. syncing from scratch), warn
		// and wait — the stake handler above will set it once the stake TX is synced.
		if validatorKey != nil {
			if err := poa.SetSigner(validatorKey); err != nil {
				logger.Warn().Err(err).Msg("Validator key not yet authorized (will activate after stake TX is synced)")
			}
		}
	}

	// ── 8c. Wire reverted-tx handler ──────────────────────────────────
	// After a reorg, reverted non-coinbase transactions are returned to the
	// mempool so they can be re-included in the new chain.
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

	// ── 9. Start RPC server ────────────────────────────────────────────
	rpcAddr := fmt.Sprintf("%s:%d", cfg.RPC.Addr, cfg.RPC.Port)
	rpcServer := rpc.New(rpcAddr, ch, utxoStore, pool, p2pNode, genesis, engine, cfg.RPC)
	if err := rpcServer.Start(); err != nil {
		logger.Fatal().Err(err).Str("addr", rpcAddr).Msg("Failed to start RPC server")
	}
	defer rpcServer.Stop()

	// Wire token store.
	tokenStore := token.NewStore(db)
	rpcServer.SetTokenStore(tokenStore)

	// Wire validator tracker.
	rpcServer.SetValidatorTracker(tracker)

	// Wire ban manager for net_getBanList.
	rpcServer.SetBanManager(p2pNode.BanManager)

	logger.Info().Str("addr", rpcServer.Addr()).Msg("RPC server started")

	// ── 9a. Wallet RPC ────────────────────────────────────────────────
	if flags.Wallet {
		ks, ksErr := wallet.NewKeystore(cfg.KeystoreDir())
		if ksErr != nil {
			logger.Fatal().Err(ksErr).Msg("Failed to create wallet keystore")
		}
		rpcServer.SetKeystore(ks)
		rpcServer.SetWalletTxIndex(rpc.NewWalletTxIndex(db))
		logger.Info().Str("path", cfg.KeystoreDir()).Msg("Wallet RPC enabled")
	}

	// ── 9b. Context for miners and startup sync ──────────────────────────
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// ── 9c. Sub-chain manager ──────────────────────────────────────────
	if genesis.Protocol.SubChain.Enabled {
		syncFilter := subchain.NewSyncFilter(cfg.SubChainSync)
		scManager, scErr := subchain.NewManager(subchain.ManagerConfig{
			ParentDB:   db,
			ParentID:   types.ChainID{},
			Rules:      &genesis.Protocol.SubChain,
			SyncFilter: syncFilter,
		})
		if scErr != nil {
			logger.Fatal().Err(scErr).Msg("Failed to create sub-chain manager")
		}

		// Build mine filter from --mine-subchains flag.
		var mineFilter *subchain.MineFilter
		if len(cfg.SubChainMineIDs) > 0 {
			if len(cfg.SubChainMineIDs) > config.MaxSubChainMiners {
				logger.Fatal().
					Int("count", len(cfg.SubChainMineIDs)).
					Int("max", config.MaxSubChainMiners).
					Msg("Too many sub-chain mine IDs (exceeds MaxSubChainMiners)")
			}
			mineFilter = subchain.NewMineFilter(cfg.SubChainMineIDs)
			logger.Info().
				Int("count", len(cfg.SubChainMineIDs)).
				Msg("Sub-chain mining filter configured")
		}

		// Resolve coinbase for sub-chain mining (from --coinbase or --validator-key).
		var scCoinbase types.Address
		if flags.Coinbase != "" || validatorKey != nil {
			scCoinbase, _ = resolveCoinbase(flags.Coinbase, validatorKey)
		}

		// Spawn handler: joins P2P topics, registers sync, runs catch-up, starts miner.
		scManager.SetSpawnHandler(func(chainID types.ChainID, sr *subchain.SpawnResult) {
			idHex := hex.EncodeToString(chainID[:])
			scLog := logger.With().Str("subchain", idHex[:8]).Logger()

			// Wire PoW DifficultyFn for block production.
			// Prepare() calls this to set header.Difficulty before mining.
			// Verification is done by the chain processor (verifyDifficulty).
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

			// Wire dynamic validator staking for PoA sub-chains with validator_stake > 0.
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

				// Recover staked validators on restart.
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

			// Create per-chain validator tracker for PoA sub-chains.
			var scTracker *consensus.ValidatorTracker
			var scPoA *consensus.PoA
			if poa, ok := sr.Engine.(*consensus.PoA); ok {
				scPoA = poa
				scTracker = consensus.NewValidatorTracker(60 * time.Second)

				// Join heartbeat topic for this sub-chain.
				if err := p2pNode.JoinSubChainHeartbeat(idHex); err != nil {
					scLog.Warn().Err(err).Msg("Failed to join sub-chain heartbeat topic")
				} else {
					scPoALocal := scPoA // capture for closure
					p2pNode.SetSubChainHeartbeatHandler(idHex, func(msg *p2p.HeartbeatMessage) {
						// Only record heartbeats from known validators.
						if scPoALocal != nil && !scPoALocal.IsValidator(msg.PubKey) {
							return
						}
						scTracker.RecordHeartbeat(msg.PubKey)
					})
					scLog.Info().Msg("Sub-chain heartbeat joined")
				}

				rpcServer.SetSubChainTracker(idHex, scTracker)
			}

			// Join P2P topics for this sub-chain.
			if err := p2pNode.JoinSubChain(idHex); err != nil {
				scLog.Warn().Err(err).Msg("Failed to join sub-chain P2P topics")
			} else {
				scLog.Info().Msg("Joined sub-chain P2P topics")
			}

			// Register sync handlers so peers can request blocks from us.
			syncer.RegisterSubChainHandler(idHex, func(from uint64, max uint32) []*block.Block {
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
			syncer.RegisterSubChainHeightHandler(idHex, func() (uint64, string) {
				return sr.Chain.Height(), sr.Chain.TipHash().String()
			})

			// Atomic flag to prevent concurrent syncs for this chain.
			var syncing atomic.Bool

			// Set block handler: process incoming blocks, trigger sync on gap.
			p2pNode.SetSubChainBlockHandler(idHex, func(from peer.ID, data []byte) {
				var blk block.Block
				if err := json.Unmarshal(data, &blk); err != nil {
					p2pNode.BanManager.RecordOffense(from, p2p.PenaltyInvalidBlock, "sc unmarshal: "+err.Error())
					return
				}
				if err := sr.Chain.ProcessBlock(&blk); err != nil {
					if errors.Is(err, chain.ErrPrevNotFound) && syncing.CompareAndSwap(false, true) {
						go func() {
							defer syncing.Store(false)
							runSubChainSync(ctx, syncer, sr.Chain, sr.Pool, p2pNode, idHex, scLog)
						}()
					} else if !errors.Is(err, chain.ErrBlockKnown) &&
						!errors.Is(err, chain.ErrPrevNotFound) &&
						!errors.Is(err, chain.ErrForkDetected) {
						p2pNode.BanManager.RecordOffense(from, p2p.PenaltyInvalidBlock, err.Error())
					}
					return
				}
				sr.Pool.RemoveConfirmed(blk.Transactions)

				// Track block signer for PoA sub-chain validator status.
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

			// Set tx handler: add to sub-chain mempool.
			p2pNode.SetSubChainTxHandler(idHex, func(from peer.ID, data []byte) {
				var t tx.Transaction
				if err := json.Unmarshal(data, &t); err != nil {
					p2pNode.BanManager.RecordOffense(from, p2p.PenaltyInvalidTx, "sc tx unmarshal: "+err.Error())
					return
				}
				if _, err := sr.Pool.Add(&t); err != nil {
					p2pNode.BanManager.RecordOffense(from, p2p.PenaltyInvalidTx, err.Error())
					return
				}
			})

			// Run catch-up sync in background.
			go runSubChainSync(ctx, syncer, sr.Chain, sr.Pool, p2pNode, idHex, scLog)

			// Start miner if PoW and explicitly listed in --mine-subchains.
			if isPoW(sr.Engine) && mineFilter != nil && mineFilter.ShouldMine(chainID) && scCoinbase != (types.Address{}) {
				startSubChainMiner(ctx, chainID, sr, scCoinbase, p2pNode, logger)
			}

			// Start PoA miner for sub-chains if this node is an authorized validator.
			if scPoA != nil && validatorKey != nil && flags.Mine {
				// SetSigner after staked validators are recovered.
				if err := scPoA.SetSigner(validatorKey); err == nil {
					coinbase, _ := resolveCoinbase(flags.Coinbase, validatorKey)
					startSubChainPoAMiner(ctx, chainID, sr, scPoA, coinbase, p2pNode, scTracker, logger)
				}
			}

			// Start heartbeat for PoA sub-chains if this node has a validator key.
			if scPoA != nil && validatorKey != nil && scPoA.IsValidator(validatorKey.PublicKey()) {
				startSubChainHeartbeat(ctx, chainID, p2pNode, validatorKey, sr.Chain, logger)
			}
		})

		// Stop handler: leaves P2P topics, removes sync handlers, stops miner.
		scManager.SetStopHandler(func(chainID types.ChainID) {
			idHex := hex.EncodeToString(chainID[:])
			stopSubChainMiner(chainID)
			stopSubChainHeartbeat(chainID)
			syncer.RemoveSubChainHandler(idHex)
			p2pNode.LeaveSubChainHeartbeat(idHex)
			p2pNode.LeaveSubChain(idHex)
			rpcServer.RemoveSubChainTracker(idHex)
			logger.Info().Str("chain", idHex[:16]+"...").Msg("Left sub-chain P2P topics")
		})

		// Restore previously registered sub-chains.
		if err := scManager.RestoreChains(); err != nil {
			logger.Warn().Err(err).Msg("Failed to restore sub-chains")
		} else if scManager.Count() > 0 {
			logger.Info().
				Int("registered", scManager.Count()).
				Int("syncing", scManager.SyncedCount()).
				Str("sync_mode", string(cfg.SubChainSync.Mode)).
				Msg("Sub-chains restored")
		}

		// Wire registration handler: new registrations in blocks → spawn sub-chains.
		ch.SetRegistrationHandler(func(txHash types.Hash, idx uint32, value uint64, data []byte, height uint64) {
			if err := scManager.HandleRegistration(txHash, idx, value, data, height); err != nil {
				logger.Warn().Err(err).
					Str("tx", txHash.String()[:16]+"...").
					Uint32("index", idx).
					Msg("Sub-chain registration failed")
			} else {
				chainID := subchain.DeriveChainID(txHash, idx)
				logger.Info().
					Str("chain_id", chainID.String()[:16]+"...").
					Uint64("height", height).
					Msg("Sub-chain registered")
			}
		})

		// Wire deregistration handler: reorgs that revert registration txs → stop sub-chains.
		ch.SetDeregistrationHandler(func(txHash types.Hash, idx uint32) {
			chainID := subchain.DeriveChainID(txHash, idx)
			if err := scManager.HandleDeregistration(txHash, idx); err != nil {
				logger.Warn().Err(err).
					Str("chain_id", chainID.String()[:16]+"...").
					Msg("Sub-chain deregistration failed")
			} else {
				logger.Info().
					Str("chain_id", chainID.String()[:16]+"...").
					Msg("Sub-chain deregistered (reorg)")
			}
		})

		rpcServer.SetSubChainManager(scManager)
		logger.Info().Msg("Sub-chain system enabled")
	}

	// ── 9d. Startup sync ──────────────────────────────────────────────
	// Run initial sync attempt, then keep a background loop running
	// that retries whenever the node falls behind.
	runStartupSync(ctx, syncer, ch, p2pNode, pool, logger)
	go runSyncLoop(ctx, syncer, ch, p2pNode, pool, logger)

	// ── 10. Start block production (if --mine) ──────────────────────────

	if flags.Mine {
		coinbaseAddr, err := resolveCoinbase(flags.Coinbase, validatorKey)
		if err != nil {
			logger.Fatal().Err(err).Msg("Failed to resolve coinbase address")
		}

		m := miner.New(ch, engine, pool, coinbaseAddr,
			genesis.Protocol.Consensus.BlockReward,
			genesis.Protocol.Consensus.MaxSupply,
			ch.Supply)
		blockTime := time.Duration(genesis.Protocol.Consensus.BlockTime) * time.Second

		logger.Info().
			Str("coinbase", hex.EncodeToString(coinbaseAddr[:])[:16]+"...").
			Uint64("reward", genesis.Protocol.Consensus.BlockReward).
			Dur("interval", blockTime).
			Msg("Block production enabled")

		// Start heartbeat immediately so peers know we're online.
		if validatorKey != nil {
			go runHeartbeat(ctx, p2pNode, validatorKey, ch, 60*time.Second, logger)
		}

		// Wait a stabilization period before mining to receive gossip
		// blocks from peers, preventing fork creation on validator restart.
		go func() {
			stabilize := 3 * blockTime
			logger.Info().Dur("delay", stabilize).Msg("Waiting for chain to stabilize before mining")
			select {
			case <-ctx.Done():
				return
			case <-time.After(stabilize):
			}
			runMiner(ctx, m, ch, pool, p2pNode, blockTime, poaEngine, tracker, logger)
		}()
	}

	// ── 11. Startup banner ──────────────────────────────────────────────
	logger.Info().
		Uint64("height", ch.Height()).
		Str("tip", ch.TipHash().String()[:16]+"...").
		Bool("mining", flags.Mine).
		Msg("Node started successfully")

	// ── 12. Wait for shutdown ───────────────────────────────────────────
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigCh
	logger.Info().Str("signal", sig.String()).Msg("Shutdown signal received")

	// Graceful shutdown: cancel miner → stop P2P → close DB (via defers).
	cancel()
	logger.Info().Msg("Goodbye!")
}

// runSyncLoop periodically checks if the node is behind its peers and syncs.
// Runs forever until ctx is cancelled.
func runSyncLoop(ctx context.Context, syncer *p2p.Syncer, ch *chain.Chain,
	p2pNode *p2p.Node, pool *mempool.Pool, logger zerolog.Logger) {

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if len(p2pNode.PeerList()) == 0 {
				continue
			}
			runStartupSync(ctx, syncer, ch, p2pNode, pool, logger)
		}
	}
}

// runStartupSync queries peers for their chain height and downloads any
// blocks the local node is missing.
func runStartupSync(ctx context.Context, syncer *p2p.Syncer, ch *chain.Chain,
	p2pNode *p2p.Node, pool *mempool.Pool, logger zerolog.Logger) {

	peers := p2pNode.PeerList()
	if len(peers) == 0 {
		logger.Info().Msg("No peers for startup sync")
		return
	}

	// Query up to 3 peers for their height.
	var bestPeer peer.ID
	var bestHeight uint64
	limit := 3
	if len(peers) < limit {
		limit = len(peers)
	}
	for _, p := range peers[:limit] {
		reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		resp, err := syncer.RequestHeight(reqCtx, p.ID)
		cancel()
		if err != nil {
			continue
		}
		if resp.Height > bestHeight {
			bestHeight = resp.Height
			bestPeer = p.ID
		}
	}

	localHeight := ch.Height()
	if bestHeight <= localHeight {
		logger.Info().Uint64("height", localHeight).Msg("Chain is up to date")
		return
	}

	total := bestHeight - localHeight
	logger.Info().
		Uint64("local", localHeight).
		Uint64("remote", bestHeight).
		Uint64("blocks", total).
		Msg("Syncing chain")

	syncStart := time.Now()

	// Batch-request blocks in chunks of 500.
	for from := localHeight + 1; from <= bestHeight; from += 500 {
		max := uint32(500)
		if from+uint64(max)-1 > bestHeight {
			max = uint32(bestHeight - from + 1)
		}

		reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		blocks, err := syncer.RequestBlocks(reqCtx, bestPeer, from, max)
		cancel()
		if err != nil {
			logger.Warn().Err(err).Uint64("from", from).Msg("Sync request failed")
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
						Msg("Fork detected during sync, resolving")
					resolveFork(ctx, syncer, ch, pool, bestPeer, blk.Header.Height, bestHeight, logger)
					return
				}
				logger.Warn().Err(err).Uint64("height", blk.Header.Height).Msg("Sync block failed")
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
		Msg("Sync complete")
}

// runSubChainSync downloads missing blocks for a sub-chain from peers.
func runSubChainSync(ctx context.Context, syncer *p2p.Syncer, ch *chain.Chain,
	pool *mempool.Pool, p2pNode *p2p.Node, chainIDHex string, logger zerolog.Logger) {

	peers := p2pNode.PeerList()
	if len(peers) == 0 {
		return
	}

	// Query up to 3 peers for their sub-chain height.
	var bestPeer peer.ID
	var bestHeight uint64
	limit := 3
	if len(peers) < limit {
		limit = len(peers)
	}
	for _, p := range peers[:limit] {
		reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		resp, err := syncer.RequestSubChainHeight(reqCtx, p.ID, chainIDHex)
		cancel()
		if err != nil {
			continue
		}
		if resp.Height > bestHeight {
			bestHeight = resp.Height
			bestPeer = p.ID
		}
	}

	localHeight := ch.Height()
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

		reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		blocks, err := syncer.RequestSubChainBlocks(reqCtx, bestPeer, chainIDHex, from, max)
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
					resolveSubChainFork(ctx, syncer, ch, pool, bestPeer, chainIDHex, blk.Header.Height, bestHeight, logger)
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

// resolveFork finds the common ancestor with a peer and downloads the peer's
// fork blocks so ProcessBlock can trigger a reorg.
// Called when sync encounters ErrPrevNotFound (peer's blocks reference a parent
// we don't have because they're on a different fork).
func resolveFork(ctx context.Context, syncer *p2p.Syncer, ch *chain.Chain,
	pool *mempool.Pool, peerID peer.ID, failedHeight, peerTip uint64, logger zerolog.Logger) {

	const maxForkDepth = 500

	// Walk backwards to find the common ancestor.
	// Start from failedHeight-1 because failedHeight is where the peer's block
	// references a parent we don't have.
	var ancestorHeight uint64
	found := false

	searchFrom := failedHeight - 1
	if searchFrom > ch.Height() {
		searchFrom = ch.Height()
	}

	lower := uint64(0)
	if searchFrom > maxForkDepth {
		lower = searchFrom - maxForkDepth
	}

	for h := searchFrom; h > lower; h-- {
		reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		peerBlocks, err := syncer.RequestBlocks(reqCtx, peerID, h, 1)
		cancel()
		if err != nil || len(peerBlocks) == 0 {
			continue
		}

		localBlk, err := ch.GetBlockByHeight(h)
		if err != nil {
			continue
		}

		if peerBlocks[0].Hash() == localBlk.Hash() {
			ancestorHeight = h
			found = true
			break
		}
	}

	if !found {
		logger.Warn().
			Uint64("searched_from", searchFrom).
			Uint64("max_depth", maxForkDepth).
			Msg("Fork resolution failed: no common ancestor found")
		return
	}

	logger.Info().
		Uint64("ancestor", ancestorHeight).
		Uint64("peer_tip", peerTip).
		Uint64("fork_blocks", peerTip-ancestorHeight).
		Msg("Common ancestor found, downloading fork blocks")

	// Download peer's fork blocks from ancestor+1 to peerTip and feed to ProcessBlock.
	for from := ancestorHeight + 1; from <= peerTip; from += 500 {
		max := uint32(500)
		if from+uint64(max)-1 > peerTip {
			max = uint32(peerTip - from + 1)
		}

		reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		blocks, err := syncer.RequestBlocks(reqCtx, peerID, from, max)
		cancel()
		if err != nil {
			logger.Warn().Err(err).Uint64("from", from).Msg("Fork sync request failed")
			return
		}

		for _, blk := range blocks {
			if err := ch.ProcessBlock(blk); err != nil {
				if errors.Is(err, chain.ErrBlockKnown) {
					continue
				}
				logger.Warn().Err(err).
					Uint64("height", blk.Header.Height).
					Msg("Fork sync block failed")
				return
			}
			pool.RemoveConfirmed(blk.Transactions)
		}
	}

	logger.Info().
		Uint64("height", ch.Height()).
		Str("tip", ch.TipHash().String()[:16]+"...").
		Msg("Fork resolved")
}

// resolveSubChainFork is the sub-chain variant of resolveFork.
func resolveSubChainFork(ctx context.Context, syncer *p2p.Syncer, ch *chain.Chain,
	pool *mempool.Pool, peerID peer.ID, chainIDHex string,
	failedHeight, peerTip uint64, logger zerolog.Logger) {

	const maxForkDepth = 500

	searchFrom := failedHeight - 1
	if searchFrom > ch.Height() {
		searchFrom = ch.Height()
	}

	lower := uint64(0)
	if searchFrom > maxForkDepth {
		lower = searchFrom - maxForkDepth
	}

	var ancestorHeight uint64
	found := false

	for h := searchFrom; h > lower; h-- {
		reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		peerBlocks, err := syncer.RequestSubChainBlocks(reqCtx, peerID, chainIDHex, h, 1)
		cancel()
		if err != nil || len(peerBlocks) == 0 {
			continue
		}

		localBlk, err := ch.GetBlockByHeight(h)
		if err != nil {
			continue
		}

		if peerBlocks[0].Hash() == localBlk.Hash() {
			ancestorHeight = h
			found = true
			break
		}
	}

	if !found {
		logger.Warn().
			Uint64("searched_from", searchFrom).
			Uint64("max_depth", maxForkDepth).
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

		reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		blocks, err := syncer.RequestSubChainBlocks(reqCtx, peerID, chainIDHex, from, max)
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

// loadValidatorKey reads a hex-encoded 32-byte private key from a file.
func loadValidatorKey(path string) (*crypto.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read key file: %w", err)
	}

	hexStr := strings.TrimSpace(string(data))
	keyBytes, err := hex.DecodeString(hexStr)
	if err != nil {
		return nil, fmt.Errorf("decode hex: %w", err)
	}

	return crypto.PrivateKeyFromBytes(keyBytes)
}

// resolveCoinbase determines the coinbase address from --coinbase flag or validator key.
// Accepts bech32, hex-prefixed, or raw hex addresses.
func resolveCoinbase(coinbaseStr string, validatorKey *crypto.PrivateKey) (types.Address, error) {
	if coinbaseStr != "" {
		addr, err := types.ParseAddress(coinbaseStr)
		if err != nil {
			return types.Address{}, fmt.Errorf("invalid coinbase address: %w", err)
		}
		return addr, nil
	}

	if validatorKey != nil {
		return crypto.AddressFromPubKey(validatorKey.PublicKey()), nil
	}

	return types.Address{}, fmt.Errorf("--mine requires --coinbase or --validator-key")
}

// createEngine builds a consensus engine from the genesis configuration.
func createEngine(genesis *config.Genesis, validatorKey *crypto.PrivateKey) (consensus.Engine, error) {
	switch genesis.Protocol.Consensus.Type {
	case config.ConsensusPoA:
		validators := make([][]byte, len(genesis.Protocol.Consensus.Validators))
		for i, v := range genesis.Protocol.Consensus.Validators {
			b, err := hex.DecodeString(v)
			if err != nil {
				return nil, fmt.Errorf("decode validator %d: %w", i, err)
			}
			validators[i] = b
		}

		poa, err := consensus.NewPoA(validators)
		if err != nil {
			return nil, fmt.Errorf("create poa: %w", err)
		}

		if validatorKey != nil {
			if err := poa.SetSigner(validatorKey); err != nil {
				return nil, fmt.Errorf("set signer: %w", err)
			}
		}

		return poa, nil

	default:
		return nil, fmt.Errorf("unsupported consensus type: %s", genesis.Protocol.Consensus.Type)
	}
}

// ── Sub-chain PoW mining ──────────────────────────────────────────────

var (
	scMinerMu sync.Mutex
	scMiners  = make(map[types.ChainID]context.CancelFunc)
)

func isPoW(engine consensus.Engine) bool {
	_, ok := engine.(*consensus.PoW)
	return ok
}

func startSubChainMiner(ctx context.Context, chainID types.ChainID,
	sr *subchain.SpawnResult, coinbase types.Address, p2pNode *p2p.Node, logger zerolog.Logger) {

	scMinerMu.Lock()
	if _, already := scMiners[chainID]; already {
		scMinerMu.Unlock()
		return
	}
	mCtx, cancel := context.WithCancel(ctx)
	scMiners[chainID] = cancel
	scMinerMu.Unlock()

	m := miner.New(sr.Chain, sr.Engine, sr.Pool, coinbase,
		sr.Genesis.Protocol.Consensus.BlockReward,
		sr.Genesis.Protocol.Consensus.MaxSupply,
		sr.Chain.Supply)

	idHex := hex.EncodeToString(chainID[:])
	blockTime := time.Duration(sr.Genesis.Protocol.Consensus.BlockTime) * time.Second
	subLogger := logger.With().Str("subchain", idHex[:8]).Logger()

	subLogger.Info().Msg("Starting PoW miner for sub-chain")
	go runSubChainMiner(mCtx, m, sr.Chain, sr.Pool, p2pNode, idHex, blockTime, subLogger)
}

func stopSubChainMiner(chainID types.ChainID) {
	scMinerMu.Lock()
	if cancel, ok := scMiners[chainID]; ok {
		cancel()
		delete(scMiners, chainID)
	}
	scMinerMu.Unlock()
}

func startSubChainPoAMiner(ctx context.Context, chainID types.ChainID,
	sr *subchain.SpawnResult, poaEng *consensus.PoA, coinbase types.Address,
	p2pNode *p2p.Node, tracker *consensus.ValidatorTracker, logger zerolog.Logger) {

	scMinerMu.Lock()
	if _, already := scMiners[chainID]; already {
		scMinerMu.Unlock()
		return
	}
	mCtx, cancel := context.WithCancel(ctx)
	scMiners[chainID] = cancel
	scMinerMu.Unlock()

	m := miner.New(sr.Chain, sr.Engine, sr.Pool, coinbase,
		sr.Genesis.Protocol.Consensus.BlockReward,
		sr.Genesis.Protocol.Consensus.MaxSupply,
		sr.Chain.Supply)

	idHex := hex.EncodeToString(chainID[:])
	blockTime := time.Duration(sr.Genesis.Protocol.Consensus.BlockTime) * time.Second
	subLogger := logger.With().Str("subchain", idHex[:8]).Logger()

	subLogger.Info().Msg("Starting PoA miner for sub-chain")
	go runSubChainPoAMiner(mCtx, m, sr.Chain, sr.Pool, p2pNode, idHex, blockTime, poaEng, tracker, subLogger)
}

func runSubChainPoAMiner(ctx context.Context, m *miner.Miner, ch *chain.Chain,
	pool *mempool.Pool, p2pNode *p2p.Node, chainIDHex string,
	blockTime time.Duration, poaEngine *consensus.PoA, tracker *consensus.ValidatorTracker,
	logger zerolog.Logger) {

	ticker := time.NewTicker(blockTime)
	defer ticker.Stop()

	gracePeriod := 2 * blockTime

	// Same warmup logic as root miner: assume selected validators are
	// online until we have accurate heartbeat data.
	warmupUntil := time.Now().Add(60 * time.Second)

	for {
		select {
		case <-ctx.Done():
			logger.Info().Msg("Sub-chain PoA miner stopped")
			return
		case <-ticker.C:
			nextHeight := ch.Height() + 1
			tipHash := ch.TipHash()

			// Non-selected validators wait a grace period, unless
			// the selected validator is known to be offline.
			if !poaEngine.IsSelected(nextHeight, tipHash) {
				selectedPubKey := poaEngine.SelectValidator(nextHeight, tipHash)

				inWarmup := time.Now().Before(warmupUntil)
				selectedOnline := tracker != nil && selectedPubKey != nil &&
					(tracker.IsOnline(selectedPubKey) || inWarmup)
				if selectedOnline {
					select {
					case <-ctx.Done():
						return
					case <-time.After(gracePeriod):
					}
					if ch.Height() >= nextHeight {
						continue
					}
				}

				// Selected validator missed — record it.
				if selectedPubKey != nil && tracker != nil {
					tracker.RecordMiss(selectedPubKey)
				}
			}

			blk, err := m.ProduceBlock()
			if err != nil {
				logger.Warn().Err(err).Msg("Block production failed")
				continue
			}
			if err := ch.ProcessBlock(blk); err != nil {
				logger.Warn().Err(err).Msg("Block rejected")
				// Evict offending mempool transactions to prevent repeated failures.
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

			if err := p2pNode.BroadcastSubChainBlock(chainIDHex, blk); err != nil {
				logger.Warn().Err(err).Msg("Failed to broadcast sub-chain block")
			}

			logger.Info().
				Uint64("height", blk.Header.Height).
				Str("hash", blk.Hash().String()[:16]+"...").
				Msg("Sub-chain block produced")
		}
	}
}

// ── Sub-chain heartbeat ──────────────────────────────────────────────

var (
	scHBMu       sync.Mutex
	scHeartbeats = make(map[types.ChainID]context.CancelFunc)
)

func startSubChainHeartbeat(ctx context.Context, chainID types.ChainID,
	p2pNode *p2p.Node, validatorKey *crypto.PrivateKey, ch *chain.Chain, logger zerolog.Logger) {

	scHBMu.Lock()
	if _, already := scHeartbeats[chainID]; already {
		scHBMu.Unlock()
		return
	}
	hCtx, cancel := context.WithCancel(ctx)
	scHeartbeats[chainID] = cancel
	scHBMu.Unlock()

	idHex := hex.EncodeToString(chainID[:])
	subLogger := logger.With().Str("subchain", idHex[:8]).Logger()

	go runSubChainHeartbeat(hCtx, p2pNode, validatorKey, ch, idHex, 60*time.Second, subLogger)
}

func stopSubChainHeartbeat(chainID types.ChainID) {
	scHBMu.Lock()
	if cancel, ok := scHeartbeats[chainID]; ok {
		cancel()
		delete(scHeartbeats, chainID)
	}
	scHBMu.Unlock()
}

func runSubChainHeartbeat(ctx context.Context, p2pNode *p2p.Node, validatorKey *crypto.PrivateKey,
	ch *chain.Chain, chainIDHex string, interval time.Duration, logger zerolog.Logger) {

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	pubKey := validatorKey.PublicKey()
	logger.Info().Dur("interval", interval).Msg("Sub-chain heartbeat started")

	// Broadcast initial heartbeat immediately.
	sendSubChainHeartbeat(p2pNode, validatorKey, pubKey, ch, chainIDHex, logger)

	for {
		select {
		case <-ctx.Done():
			logger.Info().Msg("Sub-chain heartbeat stopped")
			return
		case <-ticker.C:
			sendSubChainHeartbeat(p2pNode, validatorKey, pubKey, ch, chainIDHex, logger)
		}
	}
}

// sendSubChainHeartbeat signs and broadcasts a single heartbeat for a sub-chain.
func sendSubChainHeartbeat(p2pNode *p2p.Node, validatorKey *crypto.PrivateKey,
	pubKey []byte, ch *chain.Chain, chainIDHex string, logger zerolog.Logger) {

	ts := time.Now().Unix()
	height := ch.Height()

	data := p2p.HeartbeatSigningBytes(pubKey, height, ts)
	hash := crypto.Hash(data)
	sig, err := validatorKey.Sign(hash[:])
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

	if err := p2pNode.BroadcastSubChainHeartbeat(chainIDHex, msg); err != nil {
		logger.Debug().Err(err).Msg("Failed to broadcast sub-chain heartbeat")
	}
}

func runSubChainMiner(ctx context.Context, m *miner.Miner, ch *chain.Chain,
	pool *mempool.Pool, p2pNode *p2p.Node, chainIDHex string,
	blockTime time.Duration, logger zerolog.Logger) {

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

			if err := p2pNode.BroadcastSubChainBlock(chainIDHex, blk); err != nil {
				logger.Warn().Err(err).Msg("Failed to broadcast sub-chain block")
			}

			logger.Info().
				Uint64("height", blk.Header.Height).
				Str("hash", blk.Hash().String()[:16]+"...").
				Msg("Sub-chain block mined")
		}
	}
}

// runMiner runs the block production loop until the context is cancelled.
// If poaEngine is non-nil, the selected validator produces immediately while
// non-selected validators wait a grace period (2x block time) before producing,
// ensuring fair distribution while guaranteeing liveness.
func runMiner(ctx context.Context, m *miner.Miner, ch *chain.Chain,
	pool *mempool.Pool, p2pNode *p2p.Node, blockTime time.Duration,
	poaEngine *consensus.PoA, tracker *consensus.ValidatorTracker, logger zerolog.Logger) {

	ticker := time.NewTicker(blockTime)
	defer ticker.Stop()

	gracePeriod := 2 * blockTime

	// During warmup, always wait the grace period when not selected,
	// even without heartbeat data. This prevents fork creation before
	// we have accurate validator liveness information (heartbeat interval
	// is 60s, so the first heartbeat from peers may not arrive yet).
	warmupUntil := time.Now().Add(60 * time.Second)

	for {
		select {
		case <-ctx.Done():
			logger.Info().Msg("Block production stopped")
			return
		case <-ticker.C:
			nextHeight := ch.Height() + 1
			tipHash := ch.TipHash()

			// If PoA with validator selection, non-selected validators wait
			// a grace period before producing (lets the selected one go first).
			// Exception: if the selected validator is known to be offline
			// (no recent heartbeat), skip the grace period to maintain chain speed.
			if poaEngine != nil && !poaEngine.IsSelected(nextHeight, tipHash) {
				selectedPubKey := poaEngine.SelectValidator(nextHeight, tipHash)

				inWarmup := time.Now().Before(warmupUntil)
				selectedOnline := tracker != nil && selectedPubKey != nil &&
					(tracker.IsOnline(selectedPubKey) || inWarmup)
				if selectedOnline {
					logger.Debug().
						Uint64("height", nextHeight).
						Msg("Not selected, waiting grace period")

					select {
					case <-ctx.Done():
						return
					case <-time.After(gracePeriod):
					}

					// Re-check: if a block arrived during the grace period, skip.
					if ch.Height() >= nextHeight {
						continue
					}
				}

				// Selected validator didn't produce in time — record a miss.
				if selectedPubKey != nil && tracker != nil {
					tracker.RecordMiss(selectedPubKey)
				}
			}

			blk, err := m.ProduceBlock()
			if err != nil {
				logger.Error().Err(err).Msg("Failed to produce block")
				continue
			}

			if err := ch.ProcessBlock(blk); err != nil {
				logger.Error().Err(err).Msg("Failed to process own block")
				// Evict offending mempool transactions to prevent repeated failures.
				if errors.Is(err, chain.ErrCoinbaseNotMature) {
					for _, t := range blk.Transactions[1:] {
						pool.Remove(t.Hash())
					}
					logger.Info().Msg("Evicted mempool transactions due to coinbase maturity")
				}
				continue
			}
			pool.RemoveConfirmed(blk.Transactions)

			// Track own block for validator status.
			if poaEngine != nil && tracker != nil {
				if signer := poaEngine.IdentifySigner(blk.Header); signer != nil {
					tracker.RecordBlock(signer)
				}
			}

			if err := p2pNode.BroadcastBlock(blk); err != nil {
				logger.Error().Err(err).Msg("Failed to broadcast block")
			}

			logger.Info().
				Uint64("height", blk.Header.Height).
				Str("hash", blk.Hash().String()[:16]+"...").
				Int("txs", len(blk.Transactions)).
				Uint64("reward", blk.Transactions[0].Outputs[0].Value).
				Msg("Block produced")
		}
	}
}

// runHeartbeat broadcasts a signed heartbeat message at regular intervals.
// Only runs for mining validators (--mine --validator-key).
func runHeartbeat(ctx context.Context, p2pNode *p2p.Node, validatorKey *crypto.PrivateKey,
	ch *chain.Chain, interval time.Duration, logger zerolog.Logger) {

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	pubKey := validatorKey.PublicKey()
	logger.Info().Dur("interval", interval).Msg("Heartbeat broadcast started")

	// Broadcast initial heartbeat immediately so peers know we're online
	// without waiting for the first ticker interval.
	sendHeartbeat(p2pNode, validatorKey, pubKey, ch, logger)

	for {
		select {
		case <-ctx.Done():
			logger.Info().Msg("Heartbeat broadcast stopped")
			return
		case <-ticker.C:
			sendHeartbeat(p2pNode, validatorKey, pubKey, ch, logger)
		}
	}
}

// sendHeartbeat signs and broadcasts a single heartbeat message.
func sendHeartbeat(p2pNode *p2p.Node, validatorKey *crypto.PrivateKey,
	pubKey []byte, ch *chain.Chain, logger zerolog.Logger) {

	ts := time.Now().Unix()
	height := ch.Height()

	data := p2p.HeartbeatSigningBytes(pubKey, height, ts)
	hash := crypto.Hash(data)
	sig, err := validatorKey.Sign(hash[:])
	if err != nil {
		logger.Error().Err(err).Msg("Failed to sign heartbeat")
		return
	}

	msg := &p2p.HeartbeatMessage{
		PubKey:    pubKey,
		Height:    height,
		Timestamp: ts,
		Signature: sig,
	}

	if err := p2pNode.BroadcastHeartbeat(msg); err != nil {
		logger.Debug().Err(err).Msg("Failed to broadcast heartbeat")
	}
}
