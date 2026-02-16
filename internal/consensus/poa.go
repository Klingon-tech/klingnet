package consensus

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/Klingon-tech/klingnet-chain/pkg/block"
	"github.com/Klingon-tech/klingnet-chain/pkg/crypto"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// PoA errors.
var (
	ErrNoValidators      = errors.New("no validators configured")
	ErrNotValidator      = errors.New("signer is not an authorized validator")
	ErrMissingSig        = errors.New("block missing validator signature")
	ErrInvalidSig        = errors.New("invalid validator signature")
	ErrInsufficientStake = errors.New("validator has insufficient stake")
)

// PoA implements proof-of-authority consensus.
// Authorized validators take turns signing blocks.
type PoA struct {
	mu sync.RWMutex

	// Validators is the set of authorized validator public keys (compressed, 33 bytes).
	Validators [][]byte

	// genesisValidators is the original set from genesis (always trusted, no staking needed).
	genesisValidators [][]byte

	// signer is the local validator's private key (nil if this node is not a validator).
	signer *crypto.PrivateKey

	// stakeChecker verifies on-chain stake for non-genesis validators.
	// nil means no staking required (backward compatible).
	stakeChecker StakeChecker
}

// NewPoA creates a new PoA engine with the given validator public keys.
// These validators are treated as genesis validators (always trusted).
func NewPoA(validators [][]byte) (*PoA, error) {
	if len(validators) == 0 {
		return nil, ErrNoValidators
	}
	// Copy genesis validators so the original set is preserved.
	genesis := make([][]byte, len(validators))
	copy(genesis, validators)
	return &PoA{
		Validators:        validators,
		genesisValidators: genesis,
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

// VerifyHeader checks that the block header has a valid validator signature.
// For non-genesis validators, it also verifies on-chain stake if a StakeChecker
// is configured.
//
// Design note: VerifyHeader accepts a signature from ANY authorized validator,
// regardless of whose "turn" it is (per SelectValidator). Turn order is only
// used as a soft hint by the miner — non-selected validators wait a grace
// period before producing. This intentionally favors liveness over strict
// rotation: if the selected validator goes offline, others can still produce
// blocks without the chain halting.
func (p *PoA) VerifyHeader(header *block.Header) error {
	p.mu.RLock()
	validators := append([][]byte(nil), p.Validators...)
	genesisValidators := append([][]byte(nil), p.genesisValidators...)
	stakeChecker := p.stakeChecker
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
			return nil
		}
	}

	return ErrInvalidSig
}

// Prepare is a no-op for PoA — the header is ready for signing as-is.
func (p *PoA) Prepare(header *block.Header) error {
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
func (p *PoA) AddValidator(pubKey []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.isValidator(pubKey) {
		p.Validators = append(p.Validators, pubKey)
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

// SelectValidator returns the deterministically random validator for the given
// block height and previous block hash. Uses BLAKE3(prevHash || height) as
// entropy to derive a pseudo-random index into the validator set.
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

// IsSelected returns true if the local signer is the selected validator
// for the given block height and previous block hash.
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
