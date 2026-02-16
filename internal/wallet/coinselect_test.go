package wallet

import (
	"errors"
	"testing"

	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

func makeUTXOs(values ...uint64) []UTXO {
	utxos := make([]UTXO, len(values))
	for i, v := range values {
		utxos[i] = UTXO{
			Outpoint: types.Outpoint{TxID: types.Hash{byte(i + 1)}, Index: 0},
			Value:    v,
		}
	}
	return utxos
}

func TestSelectCoins_ExactMatch(t *testing.T) {
	utxos := makeUTXOs(1000, 2000, 3000)
	sel, err := SelectCoins(utxos, 2000)
	if err != nil {
		t.Fatalf("SelectCoins: %v", err)
	}
	if sel.Total != 2000 {
		t.Errorf("total = %d, want 2000", sel.Total)
	}
	if sel.Change != 0 {
		t.Errorf("change = %d, want 0", sel.Change)
	}
	if len(sel.Inputs) != 1 {
		t.Errorf("inputs = %d, want 1 (exact single match)", len(sel.Inputs))
	}
}

func TestSelectCoins_SingleUTXO(t *testing.T) {
	utxos := makeUTXOs(5000)
	sel, err := SelectCoins(utxos, 3000)
	if err != nil {
		t.Fatalf("SelectCoins: %v", err)
	}
	if sel.Total != 5000 {
		t.Errorf("total = %d, want 5000", sel.Total)
	}
	if sel.Change != 2000 {
		t.Errorf("change = %d, want 2000", sel.Change)
	}
}

func TestSelectCoins_MultipleUTXOs(t *testing.T) {
	// No single UTXO covers 4000, must combine.
	utxos := makeUTXOs(1000, 2000, 1500)
	sel, err := SelectCoins(utxos, 4000)
	if err != nil {
		t.Fatalf("SelectCoins: %v", err)
	}
	if sel.Total < 4000 {
		t.Errorf("total = %d, should be >= 4000", sel.Total)
	}
	if len(sel.Inputs) > 1 {
		// largest-first: 2000 + 1500 + 1000 = 4500
		if sel.Total != 4500 {
			t.Errorf("total = %d, want 4500", sel.Total)
		}
		if sel.Change != 500 {
			t.Errorf("change = %d, want 500", sel.Change)
		}
	}
}

func TestSelectCoins_PrefersLessChange(t *testing.T) {
	// Single match: 5000 (change=2000). Accumulation: 3000+2000=5000 (change=2000).
	// Both same change; single wins (fewer inputs).
	utxos := makeUTXOs(1000, 2000, 3000, 5000)
	sel, err := SelectCoins(utxos, 3000)
	if err != nil {
		t.Fatalf("SelectCoins: %v", err)
	}
	// Should pick the single UTXO of 3000 (exact match, 0 change).
	if sel.Change != 0 {
		t.Errorf("change = %d, want 0 (exact 3000 match)", sel.Change)
	}
	if len(sel.Inputs) != 1 {
		t.Errorf("inputs = %d, want 1", len(sel.Inputs))
	}
}

func TestSelectCoins_InsufficientFunds(t *testing.T) {
	utxos := makeUTXOs(1000, 2000)
	_, err := SelectCoins(utxos, 5000)
	if !errors.Is(err, ErrInsufficientFunds) {
		t.Errorf("expected ErrInsufficientFunds, got: %v", err)
	}
}

func TestSelectCoins_NoUTXOs(t *testing.T) {
	_, err := SelectCoins(nil, 1000)
	if !errors.Is(err, ErrNoUTXOs) {
		t.Errorf("expected ErrNoUTXOs, got: %v", err)
	}
}

func TestSelectCoins_ZeroTarget(t *testing.T) {
	utxos := makeUTXOs(1000)
	_, err := SelectCoins(utxos, 0)
	if err == nil {
		t.Error("zero target should fail")
	}
}

func TestSelectCoins_AllZeroValue(t *testing.T) {
	utxos := makeUTXOs(0, 0, 0)
	_, err := SelectCoins(utxos, 1000)
	if !errors.Is(err, ErrNoUTXOs) {
		t.Errorf("expected ErrNoUTXOs for all-zero UTXOs, got: %v", err)
	}
}

func TestSelectCoins_LargestFirst(t *testing.T) {
	// Target = 7000. No single UTXO covers it.
	// Largest-first: 5000 + 3000 = 8000 (change=1000).
	utxos := makeUTXOs(1000, 3000, 5000, 2000)
	sel, err := SelectCoins(utxos, 7000)
	if err != nil {
		t.Fatalf("SelectCoins: %v", err)
	}
	if sel.Total != 8000 {
		t.Errorf("total = %d, want 8000", sel.Total)
	}
	if sel.Change != 1000 {
		t.Errorf("change = %d, want 1000", sel.Change)
	}
	if len(sel.Inputs) != 2 {
		t.Errorf("inputs = %d, want 2", len(sel.Inputs))
	}
}

func TestSelectCoins_AllUTXOs(t *testing.T) {
	// Need all UTXOs to cover the target.
	utxos := makeUTXOs(1000, 2000, 3000)
	sel, err := SelectCoins(utxos, 6000)
	if err != nil {
		t.Fatalf("SelectCoins: %v", err)
	}
	if sel.Total != 6000 {
		t.Errorf("total = %d, want 6000", sel.Total)
	}
	if sel.Change != 0 {
		t.Errorf("change = %d, want 0", sel.Change)
	}
	if len(sel.Inputs) != 3 {
		t.Errorf("inputs = %d, want 3", len(sel.Inputs))
	}
}

func TestCoinSelection_Fields(t *testing.T) {
	utxos := makeUTXOs(5000)
	sel, _ := SelectCoins(utxos, 3000)
	if sel.Total != sel.Change+3000 {
		t.Error("Total should equal Change + target")
	}
}
