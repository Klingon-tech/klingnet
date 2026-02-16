package consensus

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/Klingon-tech/klingnet-chain/pkg/block"
	"github.com/Klingon-tech/klingnet-chain/pkg/crypto"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// Weighted difficulty constants (Clique-style).
// In-turn blocks get higher difficulty so they always win fork choice.
const (
	DiffInTurn  uint64 = 2
	DiffNoTurn  uint64 = 1
)

// PoA errors.
var (
	ErrNoValidators        = errors.New("no validators configured")
	ErrNotValidator        = errors.New("signer is not an authorized validator")
	ErrMissingSig          = errors.New("block missing validator signature")
	ErrInvalidSig          = errors.New("invalid validator signature")
	ErrInsufficientStake   = errors.New("validator has insufficient stake")
	ErrBadPoADifficulty = errors.New("incorrect PoA difficulty")
	ErrInvalidBlockTime = errors.New("blockTime must be > 0")
)

// PoA implements proof-of-authority consensus.
// Authorized validators take turns signing blocks using time-slot-based
// election (Aura-style) and weighted difficulty (Clique-style).
type PoA struct {
	mu sync.RWMutex

	// Validators is the set of authorized validator public keys (compressed, 33 bytes).
	Validators [][]byte

	// genesisValidators is the original set from genesis (always trusted, no staking needed).
	genesisValidators [][]byte

	// blockTime is the target seconds between blocks. Used for time-slot election:
	// validator = validators[timestamp / blockTime % N].
	blockTime int

	// signer is the local validator's private key (nil if this node is not a validator).
	signer *crypto.PrivateKey

	// stakeChecker verifies on-chain stake for non-genesis validators.
	// nil means no staking required (backward compatible).
	stakeChecker StakeChecker
}

// sortValidators sorts the validator slice by public key bytes (ascending).
// This ensures canonical ordering: all nodes agree on validator indices
// regardless of the order validators were discovered or added.
func sortValidators(validators [][]byte) {
	sort.Slice(validators, func(i, j int) bool {
		return bytes.Compare(validators[i], validators[j]) < 0
	})
}

// NewPoA creates a new PoA engine with the given validator public keys and
// block time (seconds). The blockTime is used for time-slot-based validator
// election: validator = validators[timestamp / blockTime % N].
// These validators are treated as genesis validators (always trusted).
// Validators are sorted by public key for canonical ordering.
func NewPoA(validators [][]byte, blockTime int) (*PoA, error) {
	if len(validators) == 0 {
		return nil, ErrNoValidators
	}
	if blockTime <= 0 {
		return nil, ErrInvalidBlockTime
	}
	sortValidators(validators)
	// Copy genesis validators so the original set is preserved.
	genesis := make([][]byte, len(validators))
	copy(genesis, validators)
	return &PoA{
		Validators:        validators,
		genesisValidators: genesis,
		blockTime:         blockTime,
	}, nil
}

// SetSigner sets the local validator key for block sealing.
func (p *PoA) SetSigner(key *crypto.PrivateKey) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	pub := key.PublicKey()
	if !p.isValidator(pub) {
		return ErrNotValidator
	}
	p.signer = key
	return nil
}

// GetSigner returns the current signer key, or nil if not set.
func (p *PoA) GetSigner() *crypto.PrivateKey {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.signer
}

// SetStakeChecker configures on-chain stake verification for non-genesis validators.
func (p *PoA) SetStakeChecker(sc StakeChecker) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stakeChecker = sc
}

// VerifyHeader checks that the block header has a valid validator signature
// and correct weighted difficulty.
//
// Difficulty rules (Clique-style):
//   - In-turn signer (matches time slot) → header.Difficulty must be DiffInTurn (2)
//   - Out-of-turn signer (backup)        → header.Difficulty must be DiffNoTurn (1)
//
// For non-genesis validators, it also verifies on-chain stake if a StakeChecker
// is configured.
func (p *PoA) VerifyHeader(header *block.Header) error {
	p.mu.RLock()
	validators := append([][]byte(nil), p.Validators...)
	genesisValidators := append([][]byte(nil), p.genesisValidators...)
	stakeChecker := p.stakeChecker
	blockTime := p.blockTime
	p.mu.RUnlock()

	if len(header.ValidatorSig) == 0 {
		return ErrMissingSig
	}

	hash := header.Hash()

	// Try each validator's public key.
	for _, pub := range validators {
		if crypto.VerifySignature(hash[:], header.ValidatorSig, pub) {
			// Signature valid. Check stake for non-genesis validators.
			if stakeChecker != nil && !isGenesisValidatorFromSet(genesisValidators, pub) {
				ok, err := stakeChecker.HasStake(pub)
				if err != nil {
					return fmt.Errorf("check stake: %w", err)
				}
				if !ok {
					return ErrInsufficientStake
				}
			}

			// Verify weighted difficulty matches signer's slot position.
			inTurn := slotValidatorFromSet(validators, header.Timestamp, blockTime)
			expectedDiff := DiffNoTurn
			if bytes.Equal(pub, inTurn) {
				expectedDiff = DiffInTurn
			}
			if header.Difficulty != expectedDiff {
				return fmt.Errorf("%w: signer expects %d, got %d",
					ErrBadPoADifficulty, expectedDiff, header.Difficulty)
			}

			return nil
		}
	}

	return ErrInvalidSig
}

// Prepare sets the header's weighted difficulty based on time-slot election.
// Must be called before Seal so the difficulty is included in the signed hash.
func (p *PoA) Prepare(header *block.Header) error {
	p.mu.RLock()
	validators := append([][]byte(nil), p.Validators...)
	blockTime := p.blockTime
	signer := p.signer
	p.mu.RUnlock()

	if signer == nil {
		return fmt.Errorf("no signer configured")
	}

	inTurn := slotValidatorFromSet(validators, header.Timestamp, blockTime)
	if bytes.Equal(signer.PublicKey(), inTurn) {
		header.Difficulty = DiffInTurn
	} else {
		header.Difficulty = DiffNoTurn
	}
	return nil
}

// Seal signs the block with the local validator's key.
func (p *PoA) Seal(blk *block.Block) error {
	if p.signer == nil {
		return fmt.Errorf("no signer configured")
	}

	hash := blk.Header.Hash()
	sig, err := p.signer.Sign(hash[:])
	if err != nil {
		return fmt.Errorf("seal block: %w", err)
	}
	blk.Header.ValidatorSig = sig
	return nil
}

// isValidator checks if the given public key is in the validator set.
func (p *PoA) isValidator(pubKey []byte) bool {
	for _, v := range p.Validators {
		if bytes.Equal(v, pubKey) {
			return true
		}
	}
	return false
}

// IsValidator checks if the given public key is in the validator set.
func (p *PoA) IsValidator(pubKey []byte) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.isValidator(pubKey)
}

// isGenesisValidator checks if the pubkey is in the original genesis set (internal).
func (p *PoA) isGenesisValidator(pubKey []byte) bool {
	return isGenesisValidatorFromSet(p.genesisValidators, pubKey)
}

// IsGenesisValidator checks if the pubkey is in the original genesis set.
func (p *PoA) IsGenesisValidator(pubKey []byte) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.isGenesisValidator(pubKey)
}

// AddValidator adds a new public key to the validator set.
// This allows dynamically staked validators to be accepted.
// The set is re-sorted after insertion to maintain canonical ordering.
func (p *PoA) AddValidator(pubKey []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.isValidator(pubKey) {
		p.Validators = append(p.Validators, pubKey)
		sortValidators(p.Validators)
	}
}

// RemoveValidator removes a non-genesis validator from the validator set.
// Genesis validators cannot be removed.
func (p *PoA) RemoveValidator(pubKey []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.isGenesisValidator(pubKey) {
		return
	}
	for i, v := range p.Validators {
		if bytes.Equal(v, pubKey) {
			p.Validators = append(p.Validators[:i], p.Validators[i+1:]...)
			return
		}
	}
}

// SlotValidator returns the in-turn validator for the given Unix timestamp.
// Uses Aura-style time-slot election: validator = validators[timestamp / blockTime % N].
// Selection depends only on wall clock, NOT chain tip — two nodes with synced
// clocks always agree on who's in-turn, regardless of their chain state.
func (p *PoA) SlotValidator(timestamp uint64) []byte {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return slotValidatorFromSet(p.Validators, timestamp, p.blockTime)
}

// slotValidatorFromSet returns the in-turn validator for the given timestamp.
func slotValidatorFromSet(validators [][]byte, timestamp uint64, blockTime int) []byte {
	if len(validators) == 0 {
		return nil
	}
	if len(validators) == 1 {
		return validators[0]
	}
	idx := (timestamp / uint64(blockTime)) % uint64(len(validators))
	return validators[idx]
}

// IsInTurn returns true if the local signer is the in-turn validator for the
// given Unix timestamp.
func (p *PoA) IsInTurn(timestamp uint64) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.signer == nil {
		return false
	}
	inTurn := slotValidatorFromSet(p.Validators, timestamp, p.blockTime)
	return bytes.Equal(inTurn, p.signer.PublicKey())
}

// ValidatorCount returns the number of authorized validators.
func (p *PoA) ValidatorCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.Validators)
}

// BackupDelay returns the staggered delay for out-of-turn block production.
// The delay is proportional to the signer's distance from the in-turn slot,
// so backup validators produce in a deterministic order.
// Returns 0 if the signer is in-turn or not configured.
func (p *PoA) BackupDelay(timestamp uint64) time.Duration {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.signer == nil || len(p.Validators) <= 1 {
		return 0
	}

	n := uint64(len(p.Validators))
	slot := (timestamp / uint64(p.blockTime)) % n

	// Find signer's index.
	signerIdx := int64(-1)
	pub := p.signer.PublicKey()
	for i, v := range p.Validators {
		if bytes.Equal(v, pub) {
			signerIdx = int64(i)
			break
		}
	}
	if signerIdx < 0 {
		return 0
	}

	// Distance from in-turn slot (circular).
	dist := (uint64(signerIdx) - slot + n) % n
	if dist == 0 {
		return 0 // In-turn — no delay.
	}

	// Each distance step adds blockTime/2 seconds of delay.
	// This ensures the first backup waits at least half a block time,
	// giving the in-turn block enough time to propagate via gossip
	// (GossipSub propagation is typically 1-2s).
	delayMs := dist * uint64(p.blockTime) * 500 // blockTime/2 per step, in ms
	return time.Duration(delayMs) * time.Millisecond
}

// Deprecated: SelectValidator uses tip-dependent selection. Use SlotValidator instead.
func (p *PoA) SelectValidator(height uint64, prevHash types.Hash) []byte {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return selectValidatorFromSet(p.Validators, height, prevHash)
}

func selectValidatorFromSet(validators [][]byte, height uint64, prevHash types.Hash) []byte {
	if len(validators) == 0 {
		return nil
	}
	if len(validators) == 1 {
		return validators[0]
	}

	var buf [types.HashSize + 8]byte
	copy(buf[:types.HashSize], prevHash[:])
	binary.LittleEndian.PutUint64(buf[types.HashSize:], height)
	seed := crypto.Hash(buf[:])

	idx := binary.LittleEndian.Uint64(seed[:8]) % uint64(len(validators))
	return validators[idx]
}

// IdentifySigner returns the public key of the validator that signed the block header.
// Returns nil if no validator matches. This iterates all validators because
// Schnorr signatures don't support public key recovery.
func (p *PoA) IdentifySigner(header *block.Header) []byte {
	p.mu.RLock()
	validators := append([][]byte(nil), p.Validators...)
	p.mu.RUnlock()

	if len(header.ValidatorSig) == 0 {
		return nil
	}
	hash := header.Hash()
	for _, pub := range validators {
		if crypto.VerifySignature(hash[:], header.ValidatorSig, pub) {
			return pub
		}
	}
	return nil
}

// Deprecated: IsSelected uses tip-dependent selection. Use IsInTurn instead.
func (p *PoA) IsSelected(height uint64, prevHash types.Hash) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.signer == nil {
		return false
	}
	selected := selectValidatorFromSet(p.Validators, height, prevHash)
	return bytes.Equal(selected, p.signer.PublicKey())
}

func isGenesisValidatorFromSet(genesisValidators [][]byte, pubKey []byte) bool {
	for _, v := range genesisValidators {
		if bytes.Equal(v, pubKey) {
			return true
		}
	}
	return false
}
