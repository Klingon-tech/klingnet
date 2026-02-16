package main

import (
	"fmt"
	"strconv"

	"github.com/Klingon-tech/klingnet-chain/internal/rpc"
)

// SubChainService exposes sub-chain queries to the frontend.
type SubChainService struct {
	app *App
}

// SubChainEntry describes a sub-chain in the list.
type SubChainEntry struct {
	ChainID       string `json:"chain_id"`
	Name          string `json:"name"`
	Symbol        string `json:"symbol"`
	ConsensusType string `json:"consensus_type"`
	Syncing       bool   `json:"syncing"`
	Height        uint64 `json:"height"`
	CreatedAt     uint64 `json:"created_at"`
	Balance       string `json:"balance"` // Wallet balance on this sub-chain (formatted).
}

// SubChainDetail describes a sub-chain with full details.
type SubChainDetail struct {
	ChainID        string `json:"chain_id"`
	Name           string `json:"name"`
	Symbol         string `json:"symbol"`
	ConsensusType  string `json:"consensus_type"`
	Syncing        bool   `json:"syncing"`
	Height         uint64 `json:"height"`
	TipHash        string `json:"tip_hash"`
	CreatedAt      uint64 `json:"created_at"`
	RegistrationTx string `json:"registration_tx"`
}

// SubChainListInfo is the sub-chain list result.
type SubChainListInfo struct {
	Count  int             `json:"count"`
	Chains []SubChainEntry `json:"chains"`
}

// ListSubChains returns all registered sub-chains.
// If addresses are provided, it also queries the wallet balance on each synced chain.
func (s *SubChainService) ListSubChains(addresses []string) (*SubChainListInfo, error) {
	var result rpc.SubChainListResult
	if err := s.app.rpcClient().Call("subchain_list", nil, &result); err != nil {
		return nil, err
	}

	client := s.app.rpcClient()
	chains := make([]SubChainEntry, len(result.Chains))
	for i, sc := range result.Chains {
		entry := SubChainEntry{
			ChainID:       sc.ChainID,
			Name:          sc.Name,
			Symbol:        sc.Symbol,
			ConsensusType: sc.ConsensusType,
			Syncing:       sc.Syncing,
			Height:        sc.Height,
			CreatedAt:     sc.CreatedAt,
			Balance:       "0.000000000000",
		}

		// Query balance on synced chains.
		if sc.Syncing && len(addresses) > 0 {
			var total uint64
			for _, addr := range addresses {
				var bal rpc.SubChainBalanceResult
				if err := client.Call("subchain_getBalance", rpc.SubChainBalanceParam{
					ChainID: sc.ChainID,
					Address: addr,
				}, &bal); err != nil {
					continue
				}
				total += bal.Balance
			}
			entry.Balance = formatAmount(total)
		}

		chains[i] = entry
	}
	return &SubChainListInfo{Count: result.Count, Chains: chains}, nil
}

// GetSubChainInfo returns details for a specific sub-chain.
func (s *SubChainService) GetSubChainInfo(chainID string) (*SubChainDetail, error) {
	var result rpc.SubChainInfoResult
	if err := s.app.rpcClient().Call("subchain_getInfo", rpc.ChainIDParam{ChainID: chainID}, &result); err != nil {
		return nil, err
	}
	return &SubChainDetail{
		ChainID:        result.ChainID,
		Name:           result.Name,
		Symbol:         result.Symbol,
		ConsensusType:  result.ConsensusType,
		Syncing:        result.Syncing,
		Height:         result.Height,
		TipHash:        result.TipHash,
		CreatedAt:      result.CreatedAt,
		RegistrationTx: result.RegistrationTx,
	}, nil
}

// ── Sub-chain creation ────────────────────────────────────────────────

// CreateSubChainRequest holds parameters for creating a sub-chain.
type CreateSubChainRequest struct {
	WalletName        string   `json:"wallet_name"`
	Password          string   `json:"password"`
	ChainName         string   `json:"chain_name"`
	Symbol            string   `json:"symbol"`
	ConsensusType     string   `json:"consensus_type"`
	BlockTime         int      `json:"block_time"`
	BlockReward       string   `json:"block_reward"`
	MaxSupply         string   `json:"max_supply"`
	MinFeeRate        string   `json:"min_fee_rate"`
	Validators        []string `json:"validators,omitempty"`
	InitialDifficulty uint64   `json:"initial_difficulty,omitempty"`
	DifficultyAdjust  int      `json:"difficulty_adjust,omitempty"`
}

// CreateSubChainResult is returned after a sub-chain creation tx is submitted.
type CreateSubChainResult struct {
	TxHash  string `json:"tx_hash"`
	ChainID string `json:"chain_id"`
}

// CreateSubChain creates a new sub-chain via wallet_createSubChain RPC.
func (s *SubChainService) CreateSubChain(req CreateSubChainRequest) (*CreateSubChainResult, error) {
	blockReward, err := parseAmount(req.BlockReward)
	if err != nil {
		return nil, fmt.Errorf("invalid block reward: %w", err)
	}
	maxSupply, err := parseAmount(req.MaxSupply)
	if err != nil {
		return nil, fmt.Errorf("invalid max supply: %w", err)
	}
	minFeeRate, err := strconv.ParseUint(req.MinFeeRate, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid min fee rate: %w", err)
	}
	if minFeeRate == 0 {
		return nil, fmt.Errorf("min fee rate must be > 0")
	}

	var result rpc.WalletCreateSubChainResult
	if err := s.app.rpcClient().Call("wallet_createSubChain", rpc.WalletCreateSubChainParam{
		Name:              req.WalletName,
		Password:          req.Password,
		ChainName:         req.ChainName,
		Symbol:            req.Symbol,
		ConsensusType:     req.ConsensusType,
		BlockTime:         req.BlockTime,
		BlockReward:       blockReward,
		MaxSupply:         maxSupply,
		MinFeeRate:        minFeeRate,
		Validators:        req.Validators,
		InitialDifficulty: req.InitialDifficulty,
		DifficultyAdjust:  req.DifficultyAdjust,
	}, &result); err != nil {
		return nil, err
	}
	return &CreateSubChainResult{
		TxHash:  result.TxHash,
		ChainID: result.ChainID,
	}, nil
}

// ── Sub-chain balance ─────────────────────────────────────────────────

// GetSubChainBalance returns the total balance across addresses on a sub-chain.
func (s *SubChainService) GetSubChainBalance(chainID string, addresses []string) (string, error) {
	client := s.app.rpcClient()
	var total uint64
	for _, addr := range addresses {
		var result rpc.SubChainBalanceResult
		if err := client.Call("subchain_getBalance", rpc.SubChainBalanceParam{
			ChainID: chainID,
			Address: addr,
		}, &result); err != nil {
			continue
		}
		total += result.Balance
	}
	return formatAmount(total), nil
}

// ── Sub-chain send ────────────────────────────────────────────────────

// SubChainSendRequest holds parameters for sending on a sub-chain.
type SubChainSendRequest struct {
	ChainID    string `json:"chain_id"`
	WalletName string `json:"wallet_name"`
	Password   string `json:"password"`
	ToAddress  string `json:"to_address"`
	Amount     string `json:"amount"`
}

// SubChainSendResult is returned after a sub-chain send tx is submitted.
type SubChainSendResult struct {
	TxHash string `json:"tx_hash"`
}

// SubChainConsolidateResult is returned after a sub-chain consolidation tx is submitted.
type SubChainConsolidateResult struct {
	TxHash       string `json:"tx_hash"`
	InputsUsed   uint32 `json:"inputs_used"`
	InputTotal   string `json:"input_total"`
	OutputAmount string `json:"output_amount"`
	Fee          string `json:"fee"`
}

// SubChainStakeRequest holds parameters for staking on a sub-chain.
type SubChainStakeRequest struct {
	ChainID    string `json:"chain_id"`
	WalletName string `json:"wallet_name"`
	Password   string `json:"password"`
	Amount     string `json:"amount"`
}

// SubChainStakeResult is returned after a sub-chain stake tx is submitted.
// Note: this shadows the rpc type name but is the QT-specific version with formatted amount.
type scStakeResult struct {
	TxHash string `json:"tx_hash"`
	PubKey string `json:"pubkey"`
}

// SubChainUnstakeRequest holds parameters for unstaking on a sub-chain.
type SubChainUnstakeRequest struct {
	ChainID    string `json:"chain_id"`
	WalletName string `json:"wallet_name"`
	Password   string `json:"password"`
}

// scUnstakeResult is the QT-specific unstake result with formatted amount.
type scUnstakeResult struct {
	TxHash string `json:"tx_hash"`
	Amount string `json:"amount"`
	PubKey string `json:"pubkey"`
}

// SubChainStake submits a staking transaction on a sub-chain via subchain_stake RPC.
func (s *SubChainService) SubChainStake(req SubChainStakeRequest) (*scStakeResult, error) {
	amount, err := parseAmount(req.Amount)
	if err != nil {
		return nil, fmt.Errorf("invalid amount: %w", err)
	}

	var result rpc.SubChainStakeResult
	if err := s.app.rpcClient().Call("subchain_stake", rpc.SubChainStakeParam{
		ChainID:  req.ChainID,
		Name:     req.WalletName,
		Password: req.Password,
		Amount:   amount,
	}, &result); err != nil {
		return nil, err
	}
	return &scStakeResult{TxHash: result.TxHash, PubKey: result.PubKey}, nil
}

// SubChainUnstake withdraws all stakes on a sub-chain via subchain_unstake RPC.
func (s *SubChainService) SubChainUnstake(req SubChainUnstakeRequest) (*scUnstakeResult, error) {
	var result rpc.SubChainUnstakeResult
	if err := s.app.rpcClient().Call("subchain_unstake", rpc.SubChainUnstakeParam{
		ChainID:  req.ChainID,
		Name:     req.WalletName,
		Password: req.Password,
	}, &result); err != nil {
		return nil, err
	}
	return &scUnstakeResult{
		TxHash: result.TxHash,
		Amount: formatAmount(result.Amount),
		PubKey: result.PubKey,
	}, nil
}

// SubChainSend submits a transaction on a sub-chain via subchain_send RPC.
func (s *SubChainService) SubChainSend(req SubChainSendRequest) (*SubChainSendResult, error) {
	amount, err := parseAmount(req.Amount)
	if err != nil {
		return nil, fmt.Errorf("invalid amount: %w", err)
	}

	var result rpc.SubChainSendResult
	if err := s.app.rpcClient().Call("subchain_send", rpc.SubChainSendParam{
		ChainID:  req.ChainID,
		Name:     req.WalletName,
		Password: req.Password,
		To:       req.ToAddress,
		Amount:   amount,
	}, &result); err != nil {
		return nil, err
	}
	return &SubChainSendResult{TxHash: result.TxHash}, nil
}

// SubChainConsolidate submits a consolidation transaction on a sub-chain.
func (s *SubChainService) SubChainConsolidate(chainID, walletName, password string, maxInputs uint32) (*SubChainConsolidateResult, error) {
	var result rpc.WalletConsolidateResult
	if err := s.app.rpcClient().Call("wallet_consolidate", rpc.WalletConsolidateParam{
		Name:      walletName,
		Password:  password,
		MaxInputs: maxInputs,
		ChainID:   chainID,
	}, &result); err != nil {
		return nil, err
	}
	return &SubChainConsolidateResult{
		TxHash:       result.TxHash,
		InputsUsed:   result.InputsUsed,
		InputTotal:   formatAmount(result.InputTotal),
		OutputAmount: formatAmount(result.OutputAmount),
		Fee:          formatAmount(result.Fee),
	}, nil
}
