package main

import (
	"github.com/Klingon-tech/klingnet-chain/internal/rpc"
)

// StakingService exposes validator/staking queries to the frontend.
type StakingService struct {
	app *App
}

// ValidatorInfo describes a validator.
type ValidatorInfo struct {
	PubKey    string `json:"pubkey"`
	IsGenesis bool   `json:"is_genesis"`
}

// ValidatorsInfo lists all validators.
type ValidatorsInfo struct {
	MinStake   string          `json:"min_stake"`
	Validators []ValidatorInfo `json:"validators"`
}

// StakeDetail describes a single validator's stake.
type StakeDetail struct {
	PubKey     string `json:"pubkey"`
	TotalStake string `json:"total_stake"`
	MinStake   string `json:"min_stake"`
	Sufficient bool   `json:"sufficient"`
	IsGenesis  bool   `json:"is_genesis"`
}

// GetValidators returns all validators.
func (s *StakingService) GetValidators() (*ValidatorsInfo, error) {
	var result rpc.ValidatorsResult
	if err := s.app.rpcClient().Call("stake_getValidators", nil, &result); err != nil {
		return nil, err
	}

	validators := make([]ValidatorInfo, len(result.Validators))
	for i, v := range result.Validators {
		validators[i] = ValidatorInfo{PubKey: v.PubKey, IsGenesis: v.IsGenesis}
	}
	return &ValidatorsInfo{
		MinStake:   formatAmount(result.MinStake),
		Validators: validators,
	}, nil
}

// GetStakeInfo returns stake details for a validator public key.
func (s *StakingService) GetStakeInfo(pubkey string) (*StakeDetail, error) {
	var result rpc.StakeInfoResult
	if err := s.app.rpcClient().Call("stake_getInfo", rpc.PubKeyParam{PubKey: pubkey}, &result); err != nil {
		return nil, err
	}
	return &StakeDetail{
		PubKey:     result.PubKey,
		TotalStake: formatAmount(result.TotalStake),
		MinStake:   formatAmount(result.MinStake),
		Sufficient: result.Sufficient,
		IsGenesis:  result.IsGenesis,
	}, nil
}
