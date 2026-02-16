package subchain

import (
	"encoding/json"
	"testing"

	"github.com/Klingon-tech/klingnet-chain/config"
	"github.com/Klingon-tech/klingnet-chain/pkg/tx"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

func makeRegOutput(value uint64, rd *RegistrationData) tx.Output {
	data, _ := json.Marshal(rd)
	return tx.Output{
		Value: value,
		Script: types.Script{
			Type: types.ScriptTypeRegister,
			Data: data,
		},
	}
}

func TestValidateRegistrationTx_Valid(t *testing.T) {
	rules := testRules()
	reg := NewRegistry()
	rd := validPoARegistration()
	output := makeRegOutput(50*config.Coin, rd)

	if err := ValidateRegistrationTx(output, rules, reg); err != nil {
		t.Fatalf("valid registration: %v", err)
	}
}

func TestValidateRegistrationTx_InsufficientBurn(t *testing.T) {
	rules := testRules()
	reg := NewRegistry()
	rd := validPoARegistration()
	output := makeRegOutput(1*config.Coin, rd) // Too low

	if err := ValidateRegistrationTx(output, rules, reg); err == nil {
		t.Fatal("expected error for insufficient burn")
	}
}

func TestValidateRegistrationTx_BadData(t *testing.T) {
	rules := testRules()
	reg := NewRegistry()
	output := tx.Output{
		Value: 50 * config.Coin,
		Script: types.Script{
			Type: types.ScriptTypeRegister,
			Data: []byte("not json"),
		},
	}

	if err := ValidateRegistrationTx(output, rules, reg); err == nil {
		t.Fatal("expected error for bad registration data")
	}
}

func TestValidateRegistrationTx_WrongScriptType(t *testing.T) {
	rules := testRules()
	output := tx.Output{
		Value: 50 * config.Coin,
		Script: types.Script{
			Type: types.ScriptTypeP2PKH,
			Data: []byte{},
		},
	}

	if err := ValidateRegistrationTx(output, rules, nil); err == nil {
		t.Fatal("expected error for wrong script type")
	}
}

func TestValidateRegistrationTx_MaxPerParent(t *testing.T) {
	rules := testRules()
	rules.MaxPerParent = 1
	reg := NewRegistry()
	reg.Register(&SubChain{ID: types.ChainID{1}})

	rd := validPoARegistration()
	output := makeRegOutput(50*config.Coin, rd)

	if err := ValidateRegistrationTx(output, rules, reg); err == nil {
		t.Fatal("expected error when max sub-chains reached")
	}
}
