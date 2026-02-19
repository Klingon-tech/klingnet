package rpc

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"math/big"
	"sort"
	"time"

	"github.com/Klingon-tech/klingnet-chain/config"
	"github.com/Klingon-tech/klingnet-chain/internal/consensus"
	"github.com/Klingon-tech/klingnet-chain/internal/miner"
	"github.com/Klingon-tech/klingnet-chain/internal/utxo"
	"github.com/Klingon-tech/klingnet-chain/pkg/block"
	"github.com/Klingon-tech/klingnet-chain/pkg/crypto"
	"github.com/Klingon-tech/klingnet-chain/pkg/tx"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// ── Chain endpoints ─────────────────────────────────────────────────────

func (s *Server) handleChainGetInfo(req *Request) (interface{}, *Error) {
	cc, err := s.resolveChain(extractChainID(req))
	if err != nil {
		return nil, err
	}
	return &ChainInfoResult{
		ChainID: cc.genesis.ChainID,
		Symbol:  cc.genesis.Symbol,
		Height:  cc.chain.Height(),
		TipHash: cc.chain.TipHash().String(),
	}, nil
}

func (s *Server) handleChainGetBlockByHash(req *Request) (interface{}, *Error) {
	var params HashParam
	if err := parseParams(req, &params); err != nil {
		return nil, err
	}
	if params.Hash == "" {
		return nil, &Error{Code: CodeInvalidParams, Message: "hash is required"}
	}

	cc, rpcErr := s.resolveChain(params.ChainID)
	if rpcErr != nil {
		return nil, rpcErr
	}

	hashBytes, decErr := hex.DecodeString(params.Hash)
	if decErr != nil || len(hashBytes) != types.HashSize {
		return nil, &Error{Code: CodeInvalidParams, Message: "invalid hash: must be 32-byte hex"}
	}

	var hash types.Hash
	copy(hash[:], hashBytes)

	blk, err := cc.chain.GetBlock(hash)
	if err != nil {
		return nil, &Error{Code: CodeNotFound, Message: fmt.Sprintf("block not found: %v", err)}
	}
	return NewBlockResult(blk), nil
}

func (s *Server) handleChainGetBlockByHeight(req *Request) (interface{}, *Error) {
	var params HeightParam
	if err := parseParams(req, &params); err != nil {
		return nil, err
	}

	cc, rpcErr := s.resolveChain(params.ChainID)
	if rpcErr != nil {
		return nil, rpcErr
	}

	blk, err := cc.chain.GetBlockByHeight(params.Height)
	if err != nil {
		return nil, &Error{Code: CodeNotFound, Message: fmt.Sprintf("block not found at height %d: %v", params.Height, err)}
	}
	return NewBlockResult(blk), nil
}

func (s *Server) handleChainGetTransaction(req *Request) (interface{}, *Error) {
	var params HashParam
	if err := parseParams(req, &params); err != nil {
		return nil, err
	}
	if params.Hash == "" {
		return nil, &Error{Code: CodeInvalidParams, Message: "hash is required"}
	}

	cc, rpcErr := s.resolveChain(params.ChainID)
	if rpcErr != nil {
		return nil, rpcErr
	}

	hashBytes, decErr := hex.DecodeString(params.Hash)
	if decErr != nil || len(hashBytes) != types.HashSize {
		return nil, &Error{Code: CodeInvalidParams, Message: "invalid hash: must be 32-byte hex"}
	}

	var txHash types.Hash
	copy(txHash[:], hashBytes)

	// Check mempool first.
	if t := cc.pool.Get(txHash); t != nil {
		return NewTxResult(t), nil
	}

	// Lookup via transaction index.
	t, err := cc.chain.GetTransaction(txHash)
	if err != nil {
		return nil, &Error{Code: CodeNotFound, Message: "transaction not found"}
	}
	return NewTxResult(t), nil
}

// ── UTXO endpoints ──────────────────────────────────────────────────────

func (s *Server) handleUTXOGet(req *Request) (interface{}, *Error) {
	var params OutpointParam
	if err := parseParams(req, &params); err != nil {
		return nil, err
	}
	if params.TxID == "" {
		return nil, &Error{Code: CodeInvalidParams, Message: "tx_id is required"}
	}

	cc, rpcErr := s.resolveChain(params.ChainID)
	if rpcErr != nil {
		return nil, rpcErr
	}

	txIDBytes, decErr := hex.DecodeString(params.TxID)
	if decErr != nil || len(txIDBytes) != types.HashSize {
		return nil, &Error{Code: CodeInvalidParams, Message: "invalid tx_id: must be 32-byte hex"}
	}

	var op types.Outpoint
	copy(op.TxID[:], txIDBytes)
	op.Index = params.Index

	u, err := cc.utxos.Get(op)
	if err != nil {
		return nil, &Error{Code: CodeNotFound, Message: fmt.Sprintf("utxo not found: %v", err)}
	}
	return u, nil
}

func (s *Server) handleUTXOGetByAddress(req *Request) (interface{}, *Error) {
	var params AddressParam
	if err := parseParams(req, &params); err != nil {
		return nil, err
	}
	if params.Address == "" {
		return nil, &Error{Code: CodeInvalidParams, Message: "address is required"}
	}

	cc, rpcErr := s.resolveChain(params.ChainID)
	if rpcErr != nil {
		return nil, rpcErr
	}

	addr, addrErr := decodeAddress(params.Address)
	if addrErr != nil {
		return nil, addrErr
	}

	utxos, err := cc.utxos.GetByAddress(addr)
	if err != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("get utxos: %v", err)}
	}

	return &UTXOListResult{
		Address: params.Address,
		UTXOs:   utxos,
	}, nil
}

func (s *Server) handleUTXOGetBalance(req *Request) (interface{}, *Error) {
	var params AddressParam
	if err := parseParams(req, &params); err != nil {
		return nil, err
	}
	if params.Address == "" {
		return nil, &Error{Code: CodeInvalidParams, Message: "address is required"}
	}

	cc, rpcErr := s.resolveChain(params.ChainID)
	if rpcErr != nil {
		return nil, rpcErr
	}

	addr, addrErr := decodeAddress(params.Address)
	if addrErr != nil {
		return nil, addrErr
	}

	utxos, err := cc.utxos.GetByAddress(addr)
	if err != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("get utxos: %v", err)}
	}

	// Stake UTXOs are indexed by pubkey, not address. Look up any stakes
	// belonging to this address by scanning validator pubkeys.
	stakeUTXOs, _ := stakesByAddress(cc.utxos, addr)
	utxos = append(utxos, stakeUTXOs...)

	chainHeight := cc.chain.Height()
	result := classifyUTXOs(utxos, chainHeight)
	result.Address = params.Address

	return result, nil
}

// ── Transaction endpoints ───────────────────────────────────────────────

func (s *Server) handleTxSubmit(req *Request) (interface{}, *Error) {
	var params TxSubmitParam
	if err := parseParams(req, &params); err != nil {
		return nil, err
	}
	if params.Transaction == nil {
		return nil, &Error{Code: CodeInvalidParams, Message: "transaction is required"}
	}

	cc, rpcErr := s.resolveChain(params.ChainID)
	if rpcErr != nil {
		return nil, rpcErr
	}

	_, err := cc.pool.Add(params.Transaction)
	if err != nil {
		return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("rejected: %v", err)}
	}

	// Broadcast to P2P network (root chain only — sub-chain P2P is not yet implemented).
	if params.ChainID == "" && s.p2pNode != nil {
		if err := s.p2pNode.BroadcastTx(params.Transaction); err != nil {
			s.logger.Warn().Err(err).Msg("Failed to broadcast transaction")
		}
	}

	return &TxSubmitResult{
		TxHash: params.Transaction.Hash().String(),
	}, nil
}

func (s *Server) handleTxValidate(req *Request) (interface{}, *Error) {
	var params TxSubmitParam
	if err := parseParams(req, &params); err != nil {
		return nil, err
	}
	if params.Transaction == nil {
		return nil, &Error{Code: CodeInvalidParams, Message: "transaction is required"}
	}

	cc, rpcErr := s.resolveChain(params.ChainID)
	if rpcErr != nil {
		return nil, rpcErr
	}

	adapter := miner.NewUTXOAdapter(cc.utxos)
	fee, err := params.Transaction.ValidateWithUTXOs(adapter)
	if err != nil {
		return &TxValidateResult{
			Valid: false,
			Error: err.Error(),
		}, nil
	}

	return &TxValidateResult{
		Valid: true,
		Fee:   fee,
	}, nil
}

// ── Mempool endpoints ───────────────────────────────────────────────────

func (s *Server) handleMempoolGetInfo(req *Request) (interface{}, *Error) {
	cc, err := s.resolveChain(extractChainID(req))
	if err != nil {
		return nil, err
	}
	return &MempoolInfoResult{
		Count:  cc.pool.Count(),
		MinFeeRate: cc.pool.MinFeeRate(),
	}, nil
}

func (s *Server) handleMempoolGetContent(req *Request) (interface{}, *Error) {
	cc, err := s.resolveChain(extractChainID(req))
	if err != nil {
		return nil, err
	}
	hashes := cc.pool.Hashes()
	hexHashes := make([]string, len(hashes))
	for i, h := range hashes {
		hexHashes[i] = h.String()
	}
	return &MempoolContentResult{
		Hashes: hexHashes,
	}, nil
}

// ── Network endpoints ───────────────────────────────────────────────────

func (s *Server) handleNetGetPeerInfo(_ *Request) (interface{}, *Error) {
	if s.p2pNode == nil {
		return &PeerInfoResult{Count: 0, Peers: []PeerInfo{}}, nil
	}

	peers := s.p2pNode.PeerList()
	infos := make([]PeerInfo, len(peers))
	for i, p := range peers {
		infos[i] = PeerInfo{
			ID:          p.ID.String(),
			ConnectedAt: p.ConnectedAt.UTC().Format("2006-01-02T15:04:05Z"),
		}
	}

	return &PeerInfoResult{
		Count: len(infos),
		Peers: infos,
	}, nil
}

func (s *Server) handleNetGetNodeInfo(_ *Request) (interface{}, *Error) {
	if s.p2pNode == nil {
		return &NodeInfoResult{ID: "", Addrs: []string{}}, nil
	}

	return &NodeInfoResult{
		ID:    s.p2pNode.ID().String(),
		Addrs: s.p2pNode.Addrs(),
	}, nil
}

func (s *Server) handleNetGetBanList(_ *Request) (interface{}, *Error) {
	if s.banManager == nil {
		return &BanListResult{Count: 0, Bans: []BanEntry{}}, nil
	}

	records := s.banManager.BanList()
	entries := make([]BanEntry, len(records))
	for i, r := range records {
		entries[i] = BanEntry{
			ID:        r.ID,
			Reason:    r.Reason,
			Score:     r.Score,
			BannedAt:  r.BannedAt,
			ExpiresAt: r.ExpiresAt,
		}
	}

	return &BanListResult{
		Count: len(entries),
		Bans:  entries,
	}, nil
}

// ── Staking endpoints ────────────────────────────────────────────────

func (s *Server) handleStakeGetInfo(req *Request) (interface{}, *Error) {
	var params PubKeyParam
	if err := parseParams(req, &params); err != nil {
		return nil, err
	}
	if params.PubKey == "" {
		return nil, &Error{Code: CodeInvalidParams, Message: "pubkey is required"}
	}

	pubKeyBytes, err := hex.DecodeString(params.PubKey)
	if err != nil || len(pubKeyBytes) != 33 {
		return nil, &Error{Code: CodeInvalidParams, Message: "invalid pubkey: must be 33-byte compressed hex"}
	}

	minStake := s.genesis.Protocol.Consensus.ValidatorStake

	// Check if pubkey is a genesis validator.
	isGenesis := false
	if poa, ok := s.engine.(*consensus.PoA); ok {
		for _, v := range poa.Validators {
			if hex.EncodeToString(v) == params.PubKey {
				isGenesis = poa.IsGenesisValidator(v)
				break
			}
		}
	}

	// Query stake UTXOs.
	stakes, stakeErr := s.utxos.GetStakes(pubKeyBytes)
	if stakeErr != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("get stakes: %v", stakeErr)}
	}

	var totalStake uint64
	for _, st := range stakes {
		totalStake += st.Value
	}

	sufficient := isGenesis
	if !sufficient && minStake > 0 {
		sufficient = totalStake >= minStake
	}

	return &StakeInfoResult{
		PubKey:     params.PubKey,
		TotalStake: totalStake,
		MinStake:   minStake,
		Sufficient: sufficient,
		IsGenesis:  isGenesis,
	}, nil
}

func (s *Server) handleStakeGetValidators(_ *Request) (interface{}, *Error) {
	minStake := s.genesis.Protocol.Consensus.ValidatorStake

	poa, ok := s.engine.(*consensus.PoA)
	if !ok {
		return &ValidatorsResult{MinStake: minStake, Validators: []ValidatorEntry{}}, nil
	}

	entries := make([]ValidatorEntry, len(poa.Validators))
	for i, v := range poa.Validators {
		entries[i] = ValidatorEntry{
			PubKey:    hex.EncodeToString(v),
			IsGenesis: poa.IsGenesisValidator(v),
		}
	}

	return &ValidatorsResult{
		MinStake:   minStake,
		Validators: entries,
	}, nil
}

// ── Validator status endpoints ───────────────────────────────────────

func (s *Server) handleValidatorGetStatus(req *Request) (interface{}, *Error) {
	// Resolve tracker and engine for root or sub-chain.
	chainIDHex := extractChainID(req)
	var activeTracker *consensus.ValidatorTracker
	var poa *consensus.PoA

	if chainIDHex == "" {
		// Root chain.
		activeTracker = s.tracker
		poa, _ = s.engine.(*consensus.PoA)
	} else {
		// Sub-chain.
		activeTracker = s.scTrackers[chainIDHex]
		if s.scManager != nil {
			chainIDBytes, err := hex.DecodeString(chainIDHex)
			if err == nil && len(chainIDBytes) == 32 {
				var cid [32]byte
				copy(cid[:], chainIDBytes)
				if sr, ok := s.scManager.GetChain(cid); ok {
					poa, _ = sr.Engine.(*consensus.PoA)
				}
			}
		}
	}

	if activeTracker == nil {
		return nil, &Error{Code: CodeInternalError, Message: "validator tracker not enabled"}
	}
	if poa == nil {
		return &ValidatorStatusListResult{Validators: []ValidatorStatusResult{}}, nil
	}

	// Optional pubkey filter.
	var params struct {
		PubKey  string `json:"pubkey"`
		ChainID string `json:"chain_id"`
	}
	if req.Params != nil {
		parseParams(req, &params)
	}

	if params.PubKey != "" {
		pubBytes, err := hex.DecodeString(params.PubKey)
		if err != nil || len(pubBytes) != 33 {
			return nil, &Error{Code: CodeInvalidParams, Message: "invalid pubkey: must be 33-byte hex"}
		}

		result := buildValidatorStatus(activeTracker, poa, pubBytes)
		return &ValidatorStatusListResult{
			Validators: []ValidatorStatusResult{result},
		}, nil
	}

	// Return all validators.
	results := make([]ValidatorStatusResult, len(poa.Validators))
	for i, v := range poa.Validators {
		results[i] = buildValidatorStatus(activeTracker, poa, v)
	}

	return &ValidatorStatusListResult{Validators: results}, nil
}

func buildValidatorStatus(tracker *consensus.ValidatorTracker, poa *consensus.PoA, pubKey []byte) ValidatorStatusResult {
	result := ValidatorStatusResult{
		PubKey:    hex.EncodeToString(pubKey),
		IsGenesis: poa.IsGenesisValidator(pubKey),
		IsOnline:  tracker.IsOnline(pubKey),
	}

	stats := tracker.GetStats(pubKey)
	if stats != nil {
		if !stats.LastHeartbeat.IsZero() {
			result.LastHeartbeat = stats.LastHeartbeat.Unix()
		}
		if !stats.LastBlock.IsZero() {
			result.LastBlock = stats.LastBlock.Unix()
		}
		result.BlockCount = stats.BlockCount
		result.MissedCount = stats.MissedCount
	}

	return result
}

// ── Sub-chain endpoints ─────────────────────────────────────────────

func (s *Server) handleSubChainList(_ *Request) (interface{}, *Error) {
	if s.scManager == nil {
		return &SubChainListResult{Count: 0, Chains: []SubChainInfoResult{}}, nil
	}

	chains := s.scManager.ListChains()
	results := make([]SubChainInfoResult, len(chains))
	for i, sc := range chains {
		results[i] = SubChainInfoResult{
			ChainID:           sc.ID.String(),
			Name:              sc.Name,
			Symbol:            sc.Symbol,
			ConsensusType:     sc.Registration.ConsensusType,
			BlockTime:         sc.Registration.BlockTime,
			BlockReward:       sc.Registration.BlockReward,
			MaxSupply:         sc.Registration.MaxSupply,
			MinFee:            sc.Registration.MinFeeRate,
			CreatedAt:         sc.CreatedAt,
			RegistrationTx:    sc.RegistrationTx.String(),
			InitialDifficulty: sc.Registration.InitialDifficulty,
			DifficultyAdjust:  sc.Registration.DifficultyAdjust,
		}
		// Include live height/tip if the chain instance is running.
		if sr, ok := s.scManager.GetChain(sc.ID); ok {
			results[i].Syncing = true
			results[i].Height = sr.Chain.Height()
			results[i].TipHash = sr.Chain.TipHash().String()
			// Current difficulty from the tip block header.
			if tip, err := sr.Chain.GetBlockByHeight(sr.Chain.Height()); err == nil {
				results[i].CurrentDifficulty = tip.Header.Difficulty
			}
		}
	}

	return &SubChainListResult{
		Count:  len(results),
		Chains: results,
	}, nil
}

func (s *Server) handleSubChainGetInfo(req *Request) (interface{}, *Error) {
	var params ChainIDParam
	if err := parseParams(req, &params); err != nil {
		return nil, err
	}
	if params.ChainID == "" {
		return nil, &Error{Code: CodeInvalidParams, Message: "chain_id is required"}
	}

	if s.scManager == nil {
		return nil, &Error{Code: CodeNotFound, Message: "sub-chains not enabled"}
	}

	// Parse chain ID from hex.
	chainIDBytes, decErr := hex.DecodeString(params.ChainID)
	if decErr != nil || len(chainIDBytes) != types.HashSize {
		return nil, &Error{Code: CodeInvalidParams, Message: "invalid chain_id: must be 32-byte hex"}
	}
	var chainID types.ChainID
	copy(chainID[:], chainIDBytes)

	chains := s.scManager.ListChains()
	for _, sc := range chains {
		if sc.ID == chainID {
			result := &SubChainInfoResult{
				ChainID:           sc.ID.String(),
				Name:              sc.Name,
				Symbol:            sc.Symbol,
				ConsensusType:     sc.Registration.ConsensusType,
				BlockTime:         sc.Registration.BlockTime,
				BlockReward:       sc.Registration.BlockReward,
				MaxSupply:         sc.Registration.MaxSupply,
				MinFee:            sc.Registration.MinFeeRate,
				CreatedAt:         sc.CreatedAt,
				RegistrationTx:    sc.RegistrationTx.String(),
				InitialDifficulty: sc.Registration.InitialDifficulty,
				DifficultyAdjust:  sc.Registration.DifficultyAdjust,
			}
			if sr, ok := s.scManager.GetChain(sc.ID); ok {
				result.Syncing = true
				result.Height = sr.Chain.Height()
				result.TipHash = sr.Chain.TipHash().String()
				if tip, err := sr.Chain.GetBlockByHeight(sr.Chain.Height()); err == nil {
					result.CurrentDifficulty = tip.Header.Difficulty
				}
			}
			return result, nil
		}
	}

	return nil, &Error{Code: CodeNotFound, Message: fmt.Sprintf("sub-chain %s not found", params.ChainID)}
}

// ── Sub-chain management endpoints ───────────────────────────────────

func (s *Server) requireSubChainManager() *Error {
	if s.scManager == nil {
		return &Error{Code: CodeInternalError, Message: "sub-chain manager not available"}
	}
	return nil
}

func (s *Server) handleSubChainGetBalance(req *Request) (interface{}, *Error) {
	if err := s.requireSubChainManager(); err != nil {
		return nil, err
	}

	var params SubChainBalanceParam
	if err := parseParams(req, &params); err != nil {
		return nil, err
	}
	if params.ChainID == "" {
		return nil, &Error{Code: CodeInvalidParams, Message: "chain_id is required"}
	}
	if params.Address == "" {
		return nil, &Error{Code: CodeInvalidParams, Message: "address is required"}
	}

	// Resolve sub-chain.
	chainIDBytes, decErr := hex.DecodeString(params.ChainID)
	if decErr != nil || len(chainIDBytes) != types.HashSize {
		return nil, &Error{Code: CodeInvalidParams, Message: "invalid chain_id: must be 32-byte hex"}
	}
	var chainID types.ChainID
	copy(chainID[:], chainIDBytes)

	sr, ok := s.scManager.GetChain(chainID)
	if !ok {
		return nil, &Error{Code: CodeNotFound, Message: fmt.Sprintf("sub-chain %s not synced on this node", params.ChainID)}
	}

	addr, addrErr := decodeAddress(params.Address)
	if addrErr != nil {
		return nil, addrErr
	}

	utxos, err := sr.UTXOs.GetByAddress(addr)
	if err != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("get utxos: %v", err)}
	}

	// Also include stake UTXOs for this address.
	stakeUTXOs, _ := stakesByAddress(sr.UTXOs, addr)
	utxos = append(utxos, stakeUTXOs...)

	chainHeight := sr.Chain.Height()
	bal := classifyUTXOs(utxos, chainHeight)

	return &SubChainBalanceResult{
		ChainID:   params.ChainID,
		Address:   params.Address,
		Balance:   bal.Balance,
		Spendable: bal.Spendable,
		Immature:  bal.Immature,
		Staked:    bal.Staked,
		Locked:    bal.Locked,
	}, nil
}

// ── Token endpoints ──────────────────────────────────────────────────

func (s *Server) requireTokenStore() *Error {
	if s.tokenStore == nil {
		return &Error{Code: CodeInternalError, Message: "token store not available"}
	}
	return nil
}

func (s *Server) handleTokenGetInfo(req *Request) (interface{}, *Error) {
	if err := s.requireTokenStore(); err != nil {
		return nil, err
	}

	var params TokenIDParam
	if err := parseParams(req, &params); err != nil {
		return nil, err
	}
	if params.TokenID == "" {
		return nil, &Error{Code: CodeInvalidParams, Message: "token_id is required"}
	}

	tokenIDBytes, decErr := hex.DecodeString(params.TokenID)
	if decErr != nil || len(tokenIDBytes) != types.HashSize {
		return nil, &Error{Code: CodeInvalidParams, Message: "invalid token_id: must be 32-byte hex"}
	}

	var tokenID types.TokenID
	copy(tokenID[:], tokenIDBytes)

	meta, err := s.tokenStore.Get(tokenID)
	if err != nil {
		return nil, &Error{Code: CodeNotFound, Message: fmt.Sprintf("token not found: %v", err)}
	}

	return &TokenInfoResult{
		TokenID:  params.TokenID,
		Name:     meta.Name,
		Symbol:   meta.Symbol,
		Decimals: meta.Decimals,
		Creator:  meta.Creator.String(),
	}, nil
}

func (s *Server) handleTokenGetBalance(req *Request) (interface{}, *Error) {
	var params AddressParam
	if err := parseParams(req, &params); err != nil {
		return nil, err
	}
	if params.Address == "" {
		return nil, &Error{Code: CodeInvalidParams, Message: "address is required"}
	}

	addr, addrErr := decodeAddress(params.Address)
	if addrErr != nil {
		return nil, addrErr
	}

	utxos, err := s.utxos.GetByAddress(addr)
	if err != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("get utxos: %v", err)}
	}

	// Aggregate token amounts by TokenID.
	tokenAmounts := make(map[types.TokenID]uint64)
	for _, u := range utxos {
		if u.Token != nil {
			tokenAmounts[u.Token.ID] += u.Token.Amount
		}
	}

	entries := make([]TokenBalanceEntry, 0, len(tokenAmounts))
	for tokenID, amount := range tokenAmounts {
		entry := TokenBalanceEntry{
			TokenID: hex.EncodeToString(tokenID[:]),
			Amount:  amount,
		}
		// Enrich with metadata if token store is available.
		if s.tokenStore != nil {
			if meta, err := s.tokenStore.Get(tokenID); err == nil {
				entry.Name = meta.Name
				entry.Symbol = meta.Symbol
				entry.Decimals = meta.Decimals
			}
		}
		entries = append(entries, entry)
	}

	return &TokenBalanceResult{
		Address: params.Address,
		Tokens:  entries,
	}, nil
}

func (s *Server) handleTokenList(_ *Request) (interface{}, *Error) {
	if err := s.requireTokenStore(); err != nil {
		return nil, err
	}

	list, err := s.tokenStore.List()
	if err != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("list tokens: %v", err)}
	}

	results := make([]TokenInfoResult, len(list))
	for i, entry := range list {
		results[i] = TokenInfoResult{
			TokenID:  hex.EncodeToString(entry.ID[:]),
			Name:     entry.Name,
			Symbol:   entry.Symbol,
			Decimals: entry.Decimals,
			Creator:  entry.Creator.String(),
		}
	}

	return &TokenListResult{Tokens: results}, nil
}

// ── Mining endpoints ─────────────────────────────────────────────────

func (s *Server) handleMiningGetBlockTemplate(req *Request) (interface{}, *Error) {
	if err := s.requireSubChainManager(); err != nil {
		return nil, err
	}

	var params MiningGetBlockTemplateParam
	if err := parseParams(req, &params); err != nil {
		return nil, err
	}
	if params.ChainID == "" || params.CoinbaseAddress == "" {
		return nil, &Error{Code: CodeInvalidParams, Message: "chain_id and coinbase_address are required"}
	}

	// Resolve sub-chain.
	chainIDBytes, decErr := hex.DecodeString(params.ChainID)
	if decErr != nil || len(chainIDBytes) != types.HashSize {
		return nil, &Error{Code: CodeInvalidParams, Message: "invalid chain_id: must be 32-byte hex"}
	}
	var chainID types.ChainID
	copy(chainID[:], chainIDBytes)

	sr, ok := s.scManager.GetChain(chainID)
	if !ok {
		return nil, &Error{Code: CodeNotFound, Message: fmt.Sprintf("sub-chain %s not synced on this node", params.ChainID)}
	}

	// Ensure the sub-chain uses PoW consensus.
	pow, ok := sr.Engine.(*consensus.PoW)
	if !ok {
		return nil, &Error{Code: CodeInvalidParams, Message: "sub-chain does not use PoW consensus"}
	}

	// Parse coinbase address.
	coinbaseAddr, addrErr := decodeAddress(params.CoinbaseAddress)
	if addrErr != nil {
		return nil, addrErr
	}

	// Build block template (same as miner.ProduceBlock, but skip Seal).
	var selected []*tx.Transaction
	var totalFees uint64
	if sr.Pool != nil {
		selected = sr.Pool.SelectForBlock(499) // Reserve slot for coinbase.
		for _, t := range selected {
			totalFees += sr.Pool.GetFee(t.Hash())
		}
	}

	// Cap block reward to not exceed max supply.
	reward := sr.Genesis.Protocol.Consensus.BlockReward
	maxSupply := sr.Genesis.Protocol.Consensus.MaxSupply
	if maxSupply > 0 {
		currentSupply := sr.Chain.Supply()
		if currentSupply >= maxSupply {
			reward = 0
		} else if currentSupply+reward > maxSupply {
			reward = maxSupply - currentSupply
		}
	}

	// Sort non-coinbase transactions by hash ascending (canonical order).
	sort.Slice(selected, func(i, j int) bool {
		hi, hj := selected[i].Hash(), selected[j].Hash()
		return bytes.Compare(hi[:], hj[:]) < 0
	})

	height := sr.Chain.Height() + 1
	coinbaseTx := miner.BuildCoinbase(coinbaseAddr, reward+totalFees, height)
	txs := make([]*tx.Transaction, 0, 1+len(selected))
	txs = append(txs, coinbaseTx)
	txs = append(txs, selected...)

	// Compute merkle root.
	txHashes := make([]types.Hash, len(txs))
	for i, t := range txs {
		txHashes[i] = t.Hash()
	}
	merkle := block.ComputeMerkleRoot(txHashes)

	// Ensure monotonic: template timestamp must be strictly after parent.
	// External miners may also bump the timestamp themselves; ProcessBlock
	// accepts any timestamp that is >= parent and <= now+2min.
	timestamp := uint64(time.Now().Unix())
	if parentTS := sr.Chain.TipTimestamp(); timestamp <= parentTS {
		timestamp = parentTS + 1
	}

	header := &block.Header{
		Version:    block.CurrentVersion,
		PrevHash:   sr.Chain.TipHash(),
		MerkleRoot: merkle,
		Timestamp:  timestamp,
		Height:     height,
	}

	if err := pow.Prepare(header); err != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("prepare header: %v", err)}
	}

	blk := block.NewBlock(header, txs)

	// Compute target: maxUint256 / difficulty, formatted as 64-char hex.
	targetInt := new(big.Int).Div(
		new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1)),
		new(big.Int).SetUint64(header.Difficulty),
	)
	targetHex := fmt.Sprintf("%064x", targetInt)

	return &MiningBlockTemplateResult{
		Block:      blk,
		Target:     targetHex,
		Difficulty: header.Difficulty,
		Height:     height,
		PrevHash:   sr.Chain.TipHash().String(),
	}, nil
}

func (s *Server) handleMiningSubmitBlock(req *Request) (interface{}, *Error) {
	if err := s.requireSubChainManager(); err != nil {
		return nil, err
	}

	var params MiningSubmitBlockParam
	if err := parseParams(req, &params); err != nil {
		return nil, err
	}
	if params.ChainID == "" || params.Block == nil {
		return nil, &Error{Code: CodeInvalidParams, Message: "chain_id and block are required"}
	}

	// Resolve sub-chain.
	chainIDBytes, decErr := hex.DecodeString(params.ChainID)
	if decErr != nil || len(chainIDBytes) != types.HashSize {
		return nil, &Error{Code: CodeInvalidParams, Message: "invalid chain_id: must be 32-byte hex"}
	}
	var chainID types.ChainID
	copy(chainID[:], chainIDBytes)

	sr, ok := s.scManager.GetChain(chainID)
	if !ok {
		return nil, &Error{Code: CodeNotFound, Message: fmt.Sprintf("sub-chain %s not synced on this node", params.ChainID)}
	}

	// Process the block (validates consensus, UTXO, etc.).
	if err := sr.Chain.ProcessBlock(params.Block); err != nil {
		return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("block rejected: %v", err)}
	}

	// Remove confirmed transactions from mempool.
	sr.Pool.RemoveConfirmed(params.Block.Transactions)

	// Broadcast via P2P.
	if s.p2pNode != nil {
		if err := s.p2pNode.BroadcastSubChainBlock(params.ChainID, params.Block); err != nil {
			s.logger.Warn().Err(err).Str("chain", params.ChainID).Msg("Failed to broadcast sub-chain block")
		}
	}

	blockHash := params.Block.Header.Hash()
	return &MiningSubmitBlockResult{
		BlockHash: blockHash.String(),
		Height:    params.Block.Header.Height,
	}, nil
}

// ── Helpers ─────────────────────────────────────────────────────────────

// stakesByAddress returns all stake UTXOs whose pubkey maps to the given address.
// Stake UTXOs are indexed by pubkey (not address), so we scan all staked
// validator pubkeys and derive the address for each to find a match.
func stakesByAddress(store *utxo.Store, addr types.Address) ([]*utxo.UTXO, error) {
	validators, err := store.GetAllStakedValidators()
	if err != nil {
		return nil, err
	}
	for _, pubKey := range validators {
		if crypto.AddressFromPubKey(pubKey) == addr {
			return store.GetStakes(pubKey)
		}
	}
	return nil, nil
}

// classifyUTXOs categorizes UTXOs into spendable, immature, staked, and locked.
func classifyUTXOs(utxos []*utxo.UTXO, chainHeight uint64) *BalanceResult {
	var spendable, immature, staked, locked uint64
	for _, u := range utxos {
		// Skip token UTXOs — they are a different asset.
		if u.Token != nil {
			continue
		}
		switch {
		case u.Script.Type == types.ScriptTypeStake:
			staked += u.Value
		case u.Coinbase && (chainHeight < u.Height || chainHeight-u.Height < config.CoinbaseMaturity):
			immature += u.Value
		case u.LockedUntil > 0 && chainHeight < u.LockedUntil:
			locked += u.Value
		default:
			spendable += u.Value
		}
	}
	total := spendable + immature + staked + locked
	return &BalanceResult{
		Balance:   total,
		Spendable: spendable,
		Immature:  immature,
		Staked:    staked,
		Locked:    locked,
	}
}

func decodeAddress(s string) (types.Address, *Error) {
	addr, err := types.ParseAddress(s)
	if err != nil {
		return types.Address{}, &Error{Code: CodeInvalidParams, Message: "invalid address: " + err.Error()}
	}
	return addr, nil
}
