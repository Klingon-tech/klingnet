package subchain

import (
	"encoding/json"
	"testing"

	"github.com/Klingon-tech/klingnet-chain/config"
	"github.com/Klingon-tech/klingnet-chain/internal/storage"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

func validPoARegistration() *RegistrationData {
	// 33-byte compressed pubkey = 66 hex chars: 02 + 32 bytes.
	pubkey := "02aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	return &RegistrationData{
		Name:          "Test Chain",
		Symbol:        "TST",
		ConsensusType: config.ConsensusPoA,
		BlockTime:     3,
		BlockReward:   1_000_000_000,
		MaxSupply:     1_000_000_000_000_000,
		MinFeeRate:    10,
		Validators:    []string{pubkey},
	}
}

func validPoWRegistration() *RegistrationData {
	return &RegistrationData{
		Name:              "PoW Chain",
		Symbol:            "POW",
		ConsensusType:     config.ConsensusPoW,
		BlockTime:         10,
		BlockReward:       1_000_000_000,
		MaxSupply:         1_000_000_000_000_000,
		MinFeeRate:        10,
		InitialDifficulty: 100,
	}
}

func testRules() *config.SubChainRules {
	return &config.SubChainRules{
		Enabled:          true,
		MaxDepth:         1,
		MaxPerParent:     100,
		AnchorInterval:   10,
		RequireAnchors:   true,
		AnchorTimeout:    100,
		MinDeposit:       50 * config.Coin,
		AllowPoW:         true,
		DefaultConsensus: config.ConsensusPoA,
	}
}

func TestDeriveChainID_Deterministic(t *testing.T) {
	txHash := types.Hash{1, 2, 3}
	id1 := DeriveChainID(txHash, 0)
	id2 := DeriveChainID(txHash, 0)
	if id1 != id2 {
		t.Fatalf("DeriveChainID not deterministic: %s != %s", id1, id2)
	}
}

func TestDeriveChainID_DifferentInputs(t *testing.T) {
	txHash1 := types.Hash{1}
	txHash2 := types.Hash{2}

	id1 := DeriveChainID(txHash1, 0)
	id2 := DeriveChainID(txHash2, 0)
	id3 := DeriveChainID(txHash1, 1)

	if id1 == id2 {
		t.Fatal("different tx hashes should produce different chain IDs")
	}
	if id1 == id3 {
		t.Fatal("different output indexes should produce different chain IDs")
	}
}

func TestParseRegistrationData_Valid(t *testing.T) {
	rd := validPoARegistration()
	data, err := json.Marshal(rd)
	if err != nil {
		t.Fatal(err)
	}

	parsed, err := ParseRegistrationData(data)
	if err != nil {
		t.Fatalf("ParseRegistrationData: %v", err)
	}
	if parsed.Name != rd.Name {
		t.Fatalf("Name = %q, want %q", parsed.Name, rd.Name)
	}
	if parsed.Symbol != rd.Symbol {
		t.Fatalf("Symbol = %q, want %q", parsed.Symbol, rd.Symbol)
	}
}

func TestParseRegistrationData_Invalid(t *testing.T) {
	_, err := ParseRegistrationData([]byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestValidateRegistrationData_Valid(t *testing.T) {
	rules := testRules()
	if err := ValidateRegistrationData(validPoARegistration(), rules); err != nil {
		t.Fatalf("valid PoA: %v", err)
	}
	if err := ValidateRegistrationData(validPoWRegistration(), rules); err != nil {
		t.Fatalf("valid PoW: %v", err)
	}
}

func TestValidateRegistrationData_InvalidCases(t *testing.T) {
	rules := testRules()

	tests := []struct {
		name   string
		modify func(*RegistrationData)
	}{
		{"empty name", func(r *RegistrationData) { r.Name = "" }},
		{"name too long", func(r *RegistrationData) {
			r.Name = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaX"
		}},
		{"name special chars", func(r *RegistrationData) { r.Name = "bad@name!" }},
		{"empty symbol", func(r *RegistrationData) { r.Symbol = "" }},
		{"symbol too short", func(r *RegistrationData) { r.Symbol = "X" }},
		{"symbol lowercase", func(r *RegistrationData) { r.Symbol = "abc" }},
		{"unknown consensus", func(r *RegistrationData) { r.ConsensusType = "pos" }},
		{"poa no validators", func(r *RegistrationData) { r.Validators = nil }},
		{"poa bad validator hex", func(r *RegistrationData) { r.Validators = []string{"gg"} }},
		{"poa short validator", func(r *RegistrationData) { r.Validators = []string{"0102"} }},
		{"block time zero", func(r *RegistrationData) { r.BlockTime = 0 }},
		{"block reward zero", func(r *RegistrationData) { r.BlockReward = 0 }},
		{"max supply zero", func(r *RegistrationData) { r.MaxSupply = 0 }},
		{"max supply < reward", func(r *RegistrationData) { r.MaxSupply = 1; r.BlockReward = 2 }},
		{"min fee rate zero", func(r *RegistrationData) { r.MinFeeRate = 0 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rd := validPoARegistration()
			tt.modify(rd)
			if err := ValidateRegistrationData(rd, rules); err == nil {
				t.Fatalf("expected error for %q", tt.name)
			}
		})
	}
}

func TestValidateRegistrationData_PoWNotAllowed(t *testing.T) {
	rules := testRules()
	rules.AllowPoW = false
	rd := validPoWRegistration()
	if err := ValidateRegistrationData(rd, rules); err == nil {
		t.Fatal("expected error when AllowPoW is false")
	}
}

func TestValidateRegistrationData_PoWZeroDifficulty(t *testing.T) {
	rules := testRules()
	rd := validPoWRegistration()
	rd.InitialDifficulty = 0
	if err := ValidateRegistrationData(rd, rules); err == nil {
		t.Fatal("expected error for zero difficulty")
	}
}

func TestRegistrationData_ValidatorStake_Roundtrip(t *testing.T) {
	rd := validPoARegistration()
	rd.ValidatorStake = 1000_000_000_000 // 1 coin

	data, err := json.Marshal(rd)
	if err != nil {
		t.Fatal(err)
	}

	parsed, err := ParseRegistrationData(data)
	if err != nil {
		t.Fatalf("ParseRegistrationData: %v", err)
	}
	if parsed.ValidatorStake != rd.ValidatorStake {
		t.Fatalf("ValidatorStake = %d, want %d", parsed.ValidatorStake, rd.ValidatorStake)
	}
}

func TestRegistrationData_ValidatorStake_ZeroIsValid(t *testing.T) {
	rules := testRules()
	rd := validPoARegistration()
	rd.ValidatorStake = 0 // disabled (fixed validator set)
	if err := ValidateRegistrationData(rd, rules); err != nil {
		t.Fatalf("expected ValidatorStake=0 to be valid: %v", err)
	}
}

func TestRegistrationData_ValidatorStake_OmittedInJSON(t *testing.T) {
	rd := validPoARegistration()
	rd.ValidatorStake = 0

	data, err := json.Marshal(rd)
	if err != nil {
		t.Fatal(err)
	}

	// validator_stake should be omitted when zero.
	var raw map[string]interface{}
	json.Unmarshal(data, &raw)
	if _, ok := raw["validator_stake"]; ok {
		t.Fatal("validator_stake should be omitted when zero")
	}
}

// ── Registry tests ──────────────────────────────────────────────────────

func TestRegistry_RegisterAndGet(t *testing.T) {
	reg := NewRegistry()
	sc := &SubChain{
		ID:   types.ChainID{1},
		Name: "Test",
	}
	if err := reg.Register(sc); err != nil {
		t.Fatalf("Register: %v", err)
	}

	got, ok := reg.Get(sc.ID)
	if !ok {
		t.Fatal("Get returned false")
	}
	if got.Name != "Test" {
		t.Fatalf("Name = %q, want %q", got.Name, "Test")
	}
}

func TestRegistry_DuplicateRejected(t *testing.T) {
	reg := NewRegistry()
	sc := &SubChain{ID: types.ChainID{1}}
	reg.Register(sc)
	if err := reg.Register(sc); err == nil {
		t.Fatal("expected error for duplicate registration")
	}
}

func TestRegistry_ListAndCount(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&SubChain{ID: types.ChainID{1}, Name: "A"})
	reg.Register(&SubChain{ID: types.ChainID{2}, Name: "B"})

	if reg.Count() != 2 {
		t.Fatalf("Count = %d, want 2", reg.Count())
	}
	list := reg.List()
	if len(list) != 2 {
		t.Fatalf("List len = %d, want 2", len(list))
	}
}

func TestRegistry_Has(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&SubChain{ID: types.ChainID{1}})

	if !reg.Has(types.ChainID{1}) {
		t.Fatal("Has = false, want true")
	}
	if reg.Has(types.ChainID{2}) {
		t.Fatal("Has = true for unknown, want false")
	}
}

func TestRegistry_Persistence(t *testing.T) {
	db := storage.NewMemory()
	reg := NewRegistry()

	rd := validPoARegistration()
	sc := &SubChain{
		ID:           types.ChainID{0xAA},
		Name:         "Persisted",
		Symbol:       "PER",
		CreatedAt:    42,
		Registration: *rd,
	}
	reg.Register(sc)

	// Save.
	if err := reg.SaveTo(db); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}

	// Load into a fresh registry.
	loaded, err := LoadRegistry(db)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}

	got, ok := loaded.Get(types.ChainID{0xAA})
	if !ok {
		t.Fatal("loaded registry missing chain")
	}
	if got.Name != "Persisted" {
		t.Fatalf("Name = %q, want %q", got.Name, "Persisted")
	}
	if got.Registration.Symbol != "TST" {
		t.Fatalf("Registration.Symbol = %q, want %q", got.Registration.Symbol, "TST")
	}
}
