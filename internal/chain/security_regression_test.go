package chain

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/Klingon-tech/klingnet-chain/config"
	"github.com/Klingon-tech/klingnet-chain/internal/consensus"
	"github.com/Klingon-tech/klingnet-chain/internal/token"
	"github.com/Klingon-tech/klingnet-chain/pkg/block"
	"github.com/Klingon-tech/klingnet-chain/pkg/crypto"
	"github.com/Klingon-tech/klingnet-chain/pkg/tx"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

func TestProcessBlock_RejectsForgedSpendInBlock(t *testing.T) {
	ch, _, _ := testChain(t)

	genesisBlock, err := ch.GetBlockByHeight(0)
	if err != nil {
		t.Fatalf("GetBlockByHeight(0): %v", err)
	}
	prevOut := types.Outpoint{TxID: genesisBlock.Transactions[0].Hash(), Index: 0}

	attackerKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	attackerAddr := crypto.AddressFromPubKey(attackerKey.PublicKey())

	spendBuilder := tx.NewBuilder().
		AddInput(prevOut).
		AddOutput(4000, types.Script{Type: types.ScriptTypeP2PKH, Data: attackerAddr.Bytes()})
	if err := spendBuilder.Sign(attackerKey); err != nil {
		t.Fatalf("Sign forged tx: %v", err)
	}
	forgedTx := spendBuilder.Build()

	coinbase := &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: types.Outpoint{}}},
		Outputs: []tx.Output{{
			Value:  1000,
			Script: types.Script{Type: types.ScriptTypeP2PKH, Data: attackerAddr.Bytes()},
		}},
	}

	merkle := block.ComputeMerkleRoot([]types.Hash{coinbase.Hash(), forgedTx.Hash()})
	blk := block.NewBlock(&block.Header{
		Version:    block.CurrentVersion,
		PrevHash:   ch.TipHash(),
		MerkleRoot: merkle,
		Timestamp:  1700000001,
		Height:     1,
	}, []*tx.Transaction{coinbase, forgedTx})

	poa := ch.engine.(*consensus.PoA)
	poa.Prepare(blk.Header)
	if err := poa.Seal(blk); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	err = ch.ProcessBlock(blk)
	if !errors.Is(err, tx.ErrScriptMismatch) {
		t.Fatalf("expected script mismatch, got: %v", err)
	}
}

func TestProcessBlock_RejectsCoinbaseRewardAboveConfiguredSubsidy(t *testing.T) {
	ch, _, _ := testChain(t)
	state := ch.State()

	coinbase := &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: types.Outpoint{}}},
		Outputs: []tx.Output{{
			Value:  5000, // Exceeds configured BlockReward (1000) in test genesis.
			Script: types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, types.AddressSize)},
		}},
	}

	merkle := block.ComputeMerkleRoot([]types.Hash{coinbase.Hash()})
	blk := block.NewBlock(&block.Header{
		Version:    block.CurrentVersion,
		PrevHash:   state.TipHash,
		MerkleRoot: merkle,
		Timestamp:  1700000001,
		Height:     1,
	}, []*tx.Transaction{coinbase})

	poa := ch.engine.(*consensus.PoA)
	poa.Prepare(blk.Header)
	if err := poa.Seal(blk); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	err := ch.ProcessBlock(blk)
	if !errors.Is(err, ErrCoinbaseRewardExceeded) {
		t.Fatalf("expected ErrCoinbaseRewardExceeded, got: %v", err)
	}
}

func TestProcessBlock_RejectsMintWhenDisabled(t *testing.T) {
	ch, validatorKey, _ := testChain(t)
	ch.SetTokenRules(config.TokenRules{AllowMinting: false})

	genesisBlock, err := ch.GetBlockByHeight(0)
	if err != nil {
		t.Fatalf("GetBlockByHeight(0): %v", err)
	}
	prevOut := types.Outpoint{TxID: genesisBlock.Transactions[0].Hash(), Index: 0}
	recipientAddr := crypto.AddressFromPubKey(validatorKey.PublicKey())
	tokenID := token.DeriveTokenID(prevOut.TxID, prevOut.Index)

	mintBuilder := tx.NewBuilder().
		AddInput(prevOut).
		AddTokenOutput(0, types.Script{Type: types.ScriptTypeMint, Data: recipientAddr.Bytes()}, types.TokenData{
			ID:     tokenID,
			Amount: 1,
		})
	if err := mintBuilder.Sign(validatorKey); err != nil {
		t.Fatalf("Sign mint tx: %v", err)
	}
	mintTx := mintBuilder.Build()

	coinbase := &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: types.Outpoint{}}},
		Outputs: []tx.Output{{
			Value:  1000,
			Script: types.Script{Type: types.ScriptTypeP2PKH, Data: recipientAddr.Bytes()},
		}},
	}

	blk := buildCustomBlock(t, ch, []*tx.Transaction{coinbase, mintTx})
	err = ch.ProcessBlock(blk)
	if !errors.Is(err, ErrMintingDisabled) {
		t.Fatalf("expected ErrMintingDisabled, got: %v", err)
	}
}

func TestProcessBlock_RejectsMalformedCoinbaseTx(t *testing.T) {
	ch, _, _ := testChain(t)
	state := ch.State()

	// Transaction 0 with multiple inputs should not be accepted as coinbase.
	coinbase := &tx.Transaction{
		Version: 1,
		Inputs: []tx.Input{
			{PrevOut: types.Outpoint{}},
			{
				PrevOut:   types.Outpoint{TxID: types.Hash{0x01}, Index: 0},
				Signature: []byte{0x01},
				PubKey:    []byte{0x02},
			},
		},
		Outputs: []tx.Output{{
			Value:  1000,
			Script: types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, types.AddressSize)},
		}},
	}

	merkle := block.ComputeMerkleRoot([]types.Hash{coinbase.Hash()})
	blk := block.NewBlock(&block.Header{
		Version:    block.CurrentVersion,
		PrevHash:   state.TipHash,
		MerkleRoot: merkle,
		Timestamp:  1700000001,
		Height:     1,
	}, []*tx.Transaction{coinbase})

	poa := ch.engine.(*consensus.PoA)
	poa.Prepare(blk.Header)
	if err := poa.Seal(blk); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	err := ch.ProcessBlock(blk)
	if !errors.Is(err, block.ErrNoCoinbase) {
		t.Fatalf("expected block.ErrNoCoinbase, got: %v", err)
	}
}

func TestProcessBlock_RejectsForkBlockWithInvalidHeightForParent(t *testing.T) {
	ch, _, _ := testChain(t)

	genesisHash := ch.TipHash()
	validBlock := buildCoinbaseBlock(t, ch, genesisHash, 1, types.Address{}, 0)
	if err := ch.ProcessBlock(validBlock); err != nil {
		t.Fatalf("process valid block: %v", err)
	}

	coinbase := &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: types.Outpoint{}}},
		Outputs: []tx.Output{{
			Value:  1000,
			Script: types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, types.AddressSize)},
		}},
	}
	merkle := block.ComputeMerkleRoot([]types.Hash{coinbase.Hash()})
	blk := block.NewBlock(&block.Header{
		Version:    block.CurrentVersion,
		PrevHash:   genesisHash, // Known parent, but not current tip.
		MerkleRoot: merkle,
		Timestamp:  1700000008,
		Height:     5, // Invalid: genesis parent requires height 1.
	}, []*tx.Transaction{coinbase})

	poa := ch.engine.(*consensus.PoA)
	poa.Prepare(blk.Header)
	if err := poa.Seal(blk); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	err := ch.ProcessBlock(blk)
	if !errors.Is(err, ErrBadHeight) {
		t.Fatalf("expected ErrBadHeight, got: %v", err)
	}
}

func TestProcessBlock_RejectsSupplyOverflow(t *testing.T) {
	ch, _, _ := testChain(t)

	// Force state near uint64 limit. Any positive reward should overflow.
	ch.state.Supply = math.MaxUint64
	ch.maxSupply = 0 // Unlimited cap so overflow guard is exercised.

	coinbase := &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: types.Outpoint{}}},
		Outputs: []tx.Output{{
			Value:  1,
			Script: types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, types.AddressSize)},
		}},
	}
	blk := buildCustomBlock(t, ch, []*tx.Transaction{coinbase})

	err := ch.ProcessBlock(blk)
	if err == nil || !strings.Contains(err.Error(), "supply overflow") {
		t.Fatalf("expected supply overflow error, got: %v", err)
	}
}

func TestProcessBlock_RejectsCumulativeDifficultyOverflow(t *testing.T) {
	ch, _, _ := testChain(t)

	// Force cumulative difficulty near uint64 limit.
	ch.state.CumulativeDifficulty = math.MaxUint64

	coinbase := &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: types.Outpoint{}}},
		Outputs: []tx.Output{{
			Value:  1,
			Script: types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, types.AddressSize)},
		}},
	}
	blk := buildCustomBlock(t, ch, []*tx.Transaction{coinbase})

	err := ch.ProcessBlock(blk)
	if err == nil || !strings.Contains(err.Error(), "cumulative difficulty overflow") {
		t.Fatalf("expected cumulative difficulty overflow error, got: %v", err)
	}
}

func TestProcessBlock_RejectsCoinbaseAboveHalvedSubsidy(t *testing.T) {
	ch, _, _ := testChain(t)
	ch.SetConsensusRules(config.ConsensusRules{
		Type:            config.ConsensusPoA,
		BlockTime:       3,
		BlockReward:     1000,
		HalvingInterval: 1,
	})

	addr := types.Address{}
	blk1 := buildCoinbaseBlock(t, ch, ch.TipHash(), 1, addr, 0)
	if err := ch.ProcessBlock(blk1); err != nil {
		t.Fatalf("process first block: %v", err)
	}

	blk2 := buildCoinbaseBlock(t, ch, blk1.Hash(), 2, addr, 0)
	err := ch.ProcessBlock(blk2)
	if !errors.Is(err, ErrCoinbaseRewardExceeded) {
		t.Fatalf("expected ErrCoinbaseRewardExceeded after halving, got: %v", err)
	}
}

func TestProcessBlock_RejectsMalformedRegistrationBeforeCommit(t *testing.T) {
	ch, validatorKey, _ := testChain(t)

	errMalformedRegistration := errors.New("malformed registration")
	ch.SetRegistrationValidator(func(output tx.Output, existingRegistrations, pendingRegistrations uint64) error {
		if !json.Valid(output.Script.Data) {
			return errMalformedRegistration
		}
		return nil
	})

	genesisBlock, err := ch.GetBlockByHeight(0)
	if err != nil {
		t.Fatalf("GetBlockByHeight(0): %v", err)
	}
	prevOut := types.Outpoint{TxID: genesisBlock.Transactions[0].Hash(), Index: 0}

	registerBuilder := tx.NewBuilder().
		AddInput(prevOut).
		AddOutput(1000, types.Script{Type: types.ScriptTypeRegister, Data: []byte("not json")})
	if err := registerBuilder.Sign(validatorKey); err != nil {
		t.Fatalf("Sign registration tx: %v", err)
	}
	registerTx := registerBuilder.Build()

	blk := buildCustomBlock(t, ch, []*tx.Transaction{testCoinbaseTx(), registerTx})
	err = ch.ProcessBlock(blk)
	if !errors.Is(err, errMalformedRegistration) {
		t.Fatalf("expected malformed registration error, got: %v", err)
	}

	has, hasErr := ch.utxos.Has(types.Outpoint{TxID: registerTx.Hash(), Index: 0})
	if hasErr != nil {
		t.Fatalf("Has rejected registration output: %v", hasErr)
	}
	if has {
		t.Fatal("rejected registration output was committed to the UTXO set")
	}
	if ch.Height() != 0 {
		t.Fatalf("height = %d, want 0 after rejected registration block", ch.Height())
	}
}

func TestProcessBlock_RejectsRegistrationPastConfiguredLimit(t *testing.T) {
	ch, validatorKey, _ := testChain(t)

	errRegistrationLimit := errors.New("registration limit reached")
	ch.SetRegistrationValidator(func(output tx.Output, existingRegistrations, pendingRegistrations uint64) error {
		if existingRegistrations+pendingRegistrations >= 1 {
			return errRegistrationLimit
		}
		return nil
	})

	genesisBlock, err := ch.GetBlockByHeight(0)
	if err != nil {
		t.Fatalf("GetBlockByHeight(0): %v", err)
	}
	genesisOut := types.Outpoint{TxID: genesisBlock.Transactions[0].Hash(), Index: 0}
	validatorAddr := crypto.AddressFromPubKey(validatorKey.PublicKey())

	firstRegisterBuilder := tx.NewBuilder().
		AddInput(genesisOut).
		AddOutput(1000, types.Script{Type: types.ScriptTypeRegister, Data: []byte(`{"chain":"one"}`)}).
		AddOutput(3900, types.Script{Type: types.ScriptTypeP2PKH, Data: validatorAddr.Bytes()})
	if err := firstRegisterBuilder.Sign(validatorKey); err != nil {
		t.Fatalf("Sign first registration tx: %v", err)
	}
	firstRegisterTx := firstRegisterBuilder.Build()

	blk1 := buildCustomBlock(t, ch, []*tx.Transaction{testCoinbaseTx(), firstRegisterTx})
	if err := ch.ProcessBlock(blk1); err != nil {
		t.Fatalf("process first registration block: %v", err)
	}

	changeOut := types.Outpoint{TxID: firstRegisterTx.Hash(), Index: 1}
	secondRegisterBuilder := tx.NewBuilder().
		AddInput(changeOut).
		AddOutput(1000, types.Script{Type: types.ScriptTypeRegister, Data: []byte(`{"chain":"two"}`)}).
		AddOutput(2800, types.Script{Type: types.ScriptTypeP2PKH, Data: validatorAddr.Bytes()})
	if err := secondRegisterBuilder.Sign(validatorKey); err != nil {
		t.Fatalf("Sign second registration tx: %v", err)
	}
	secondRegisterTx := secondRegisterBuilder.Build()

	blk2 := buildCustomBlock(t, ch, []*tx.Transaction{testCoinbaseTx(), secondRegisterTx})
	err = ch.ProcessBlock(blk2)
	if !errors.Is(err, errRegistrationLimit) {
		t.Fatalf("expected registration limit error, got: %v", err)
	}

	has, hasErr := ch.utxos.Has(types.Outpoint{TxID: secondRegisterTx.Hash(), Index: 0})
	if hasErr != nil {
		t.Fatalf("Has rejected second registration output: %v", hasErr)
	}
	if has {
		t.Fatal("rejected second registration output was committed to the UTXO set")
	}
	if ch.Height() != 1 {
		t.Fatalf("height = %d, want 1 after rejected second registration block", ch.Height())
	}
}
