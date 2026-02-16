package chain

import (
	"errors"
	"testing"

	"github.com/Klingon-tech/klingnet-chain/internal/consensus"
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
