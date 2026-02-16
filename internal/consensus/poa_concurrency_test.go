package consensus

import (
	"sync"
	"testing"

	"github.com/Klingon-tech/klingnet-chain/pkg/crypto"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

func TestPoA_ConcurrentValidatorSetAccess(t *testing.T) {
	key1, _ := crypto.GenerateKey()
	key2, _ := crypto.GenerateKey()

	poa, err := NewPoA([][]byte{key1.PublicKey()}, 3)
	if err != nil {
		t.Fatalf("NewPoA: %v", err)
	}
	if err := poa.SetSigner(key1); err != nil {
		t.Fatalf("SetSigner: %v", err)
	}

	blk := testBlock(t)
	poa.Prepare(blk.Header)
	if err := poa.Seal(blk); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			poa.AddValidator(key2.PublicKey())
			poa.RemoveValidator(key2.PublicKey())
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			_ = poa.VerifyHeader(blk.Header)
			_ = poa.IdentifySigner(blk.Header)
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			_ = poa.IsValidator(key1.PublicKey())
			_ = poa.IsSelected(uint64(i), types.Hash{0x01})
		}
	}()

	wg.Wait()
}
