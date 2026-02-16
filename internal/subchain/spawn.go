package subchain

import (
	"encoding/hex"
	"fmt"

	"github.com/Klingon-tech/klingnet-chain/config"
	"github.com/Klingon-tech/klingnet-chain/internal/chain"
	"github.com/Klingon-tech/klingnet-chain/internal/consensus"
	"github.com/Klingon-tech/klingnet-chain/internal/mempool"
	"github.com/Klingon-tech/klingnet-chain/internal/miner"
	"github.com/Klingon-tech/klingnet-chain/internal/storage"
	"github.com/Klingon-tech/klingnet-chain/internal/utxo"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// SpawnConfig holds everything needed to create a sub-chain instance.
type SpawnConfig struct {
	ChainID         types.ChainID
	Registration    *RegistrationData
	ParentDB        storage.DB
	CreatedAtHeight uint64
}

// SpawnResult holds the spawned sub-chain components.
type SpawnResult struct {
	Chain   *chain.Chain
	Pool    *mempool.Pool
	Engine  consensus.Engine
	Genesis *config.Genesis
	UTXOs   *utxo.Store
	DB      storage.DB // PrefixDB for this sub-chain
}

// Spawn creates a new sub-chain instance from a registration.
// It creates a PrefixDB, builds genesis from the registration data,
// creates the consensus engine, UTXO store, chain, and mempool.
func Spawn(cfg SpawnConfig) (*SpawnResult, error) {
	if cfg.Registration == nil {
		return nil, fmt.Errorf("registration data is nil")
	}

	// Create isolated storage via PrefixDB.
	prefix := []byte("sc/" + hex.EncodeToString(cfg.ChainID[:]) + "/")
	db := storage.NewPrefixDB(cfg.ParentDB, prefix)

	// Build genesis config from registration data.
	gen := buildGenesis(cfg.ChainID, cfg.Registration, cfg.CreatedAtHeight)

	// Create consensus engine.
	engine, err := buildEngine(cfg.Registration)
	if err != nil {
		return nil, fmt.Errorf("create consensus engine: %w", err)
	}

	// Create UTXO store.
	utxoStore := utxo.NewStore(db)

	// Wire stake checker for PoA sub-chains with dynamic validators.
	if poa, ok := engine.(*consensus.PoA); ok && cfg.Registration.ValidatorStake > 0 {
		sc := consensus.NewUTXOStakeChecker(utxoStore, cfg.Registration.ValidatorStake)
		poa.SetStakeChecker(sc)
	}

	// Create chain.
	ch, err := chain.New(cfg.ChainID, db, utxoStore, engine)
	if err != nil {
		return nil, fmt.Errorf("create chain: %w", err)
	}
	ch.SetConsensusRules(gen.Protocol.Consensus)

	// Initialize from genesis if this is a fresh chain.
	state := ch.State()
	if state.IsGenesis() {
		if err := ch.InitFromGenesis(gen); err != nil {
			return nil, fmt.Errorf("init from genesis: %w", err)
		}
	}

	// Create mempool.
	adapter := miner.NewUTXOAdapter(utxoStore)
	pool := mempool.New(adapter, 0)
	pool.SetMinFeeRate(cfg.Registration.MinFeeRate)
	pool.SetCoinbaseMaturity(config.CoinbaseMaturity, ch.Height, utxoStore)
	if cfg.Registration.ValidatorStake > 0 {
		pool.SetStakeAmount(cfg.Registration.ValidatorStake)
	}

	return &SpawnResult{
		Chain:   ch,
		Pool:    pool,
		Engine:  engine,
		Genesis: gen,
		UTXOs:   utxoStore,
		DB:      db,
	}, nil
}

// buildGenesis creates a config.Genesis from RegistrationData.
// The timestamp is set to createdAtHeight to ensure deterministic genesis
// across all nodes (time.Now() would vary per node → different genesis hash).
func buildGenesis(chainID types.ChainID, reg *RegistrationData, createdAtHeight uint64) *config.Genesis {
	return &config.Genesis{
		ChainID:   chainID.String(),
		ChainName: reg.Name,
		Symbol:    reg.Symbol,
		Timestamp: createdAtHeight,
		Alloc:     map[string]uint64{}, // Sub-chains start empty; coins come from mining
		Protocol: config.ProtocolConfig{
			Consensus: config.ConsensusRules{
				Type:              reg.ConsensusType,
				BlockTime:         reg.BlockTime,
				Validators:        reg.Validators,
				InitialDifficulty: reg.InitialDifficulty,
				DifficultyAdjust:  reg.DifficultyAdjust,
				BlockReward:       reg.BlockReward,
				MaxSupply:         reg.MaxSupply,
				MinFeeRate:        reg.MinFeeRate,
				ValidatorStake:    reg.ValidatorStake,
			},
			SubChain: config.SubChainRules{
				Enabled: false, // No sub-sub-chains (flat model)
			},
			Token: config.TokenRules{
				MaxTokensPerUTXO: 1,
				AllowMinting:     true,
			},
		},
	}
}

// buildEngine creates a consensus.Engine from RegistrationData.
func buildEngine(reg *RegistrationData) (consensus.Engine, error) {
	switch reg.ConsensusType {
	case config.ConsensusPoA:
		validators := make([][]byte, len(reg.Validators))
		for i, v := range reg.Validators {
			b, err := hex.DecodeString(v)
			if err != nil {
				return nil, fmt.Errorf("decode validator %d: %w", i, err)
			}
			validators[i] = b
		}
		return consensus.NewPoA(validators)

	case config.ConsensusPoW:
		return consensus.NewPoW(reg.InitialDifficulty, reg.DifficultyAdjust, reg.BlockTime)

	default:
		return nil, fmt.Errorf("unsupported consensus type: %s", reg.ConsensusType)
	}
}
