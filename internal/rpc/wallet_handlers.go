package rpc

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/Klingon-tech/klingnet-chain/config"
	"github.com/Klingon-tech/klingnet-chain/internal/subchain"
	"github.com/Klingon-tech/klingnet-chain/internal/token"
	"github.com/Klingon-tech/klingnet-chain/internal/utxo"
	"github.com/Klingon-tech/klingnet-chain/internal/wallet"
	"github.com/Klingon-tech/klingnet-chain/pkg/crypto"
	"github.com/Klingon-tech/klingnet-chain/pkg/tx"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// Token metadata validation patterns (same style as sub-chain registration).
var (
	tokenNamePattern   = regexp.MustCompile(`^[a-zA-Z0-9 \-]{1,64}$`)
	tokenSymbolPattern = regexp.MustCompile(`^[A-Z0-9]{2,10}$`)
)

// utxoGetter abstracts UTXO retrieval by address (root or sub-chain store).
type utxoGetter interface {
	GetByAddress(types.Address) ([]*utxo.UTXO, error)
}

// walletUTXOSet holds aggregated UTXOs from all wallet addresses with signing info.
type walletUTXOSet struct {
	utxos           []wallet.UTXO
	signers         map[types.Address]*crypto.PrivateKey
	addrByOutpoint  map[types.Outpoint]types.Address
	spendableNative uint64
	immatureNative  uint64
	lockedNative    uint64
}

// zeroSigners zeroes all private keys in the wallet UTXO set and removes them from the map.
func (wset *walletUTXOSet) zeroSigners() {
	for addr, key := range wset.signers {
		key.Zero()
		delete(wset.signers, addr)
	}
}

// collectWalletUTXOs gathers UTXOs from all known wallet addresses (external + change).
// Immature coinbase outputs and locked outputs are excluded based on currentHeight.
func (s *Server) collectWalletUTXOs(
	master *wallet.HDKey,
	walletName string,
	store utxoGetter,
	currentHeight uint64,
) (*walletUTXOSet, error) {
	accounts, err := s.keystore.ListAccounts(walletName)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}

	wset := &walletUTXOSet{
		signers:        make(map[types.Address]*crypto.PrivateKey),
		addrByOutpoint: make(map[types.Outpoint]types.Address),
	}

	// If no accounts yet (brand new wallet), fall back to account 0.
	if len(accounts) == 0 {
		accounts = []wallet.AccountEntry{{Index: 0, Name: "Default"}}
	}

	for _, acct := range accounts {
		// Use the stored address for UTXO lookup (authoritative).
		addr, parseErr := types.ParseAddress(acct.Address)
		if parseErr != nil {
			continue
		}

		utxos, utxoErr := store.GetByAddress(addr)
		if utxoErr != nil || len(utxos) == 0 {
			continue
		}

		// Derive signer lazily (only if this address has UTXOs).
		if _, exists := wset.signers[addr]; !exists {
			change, index := acct.Derivation()
			hdKey, derErr := master.DeriveAddress(0, change, index)
			if derErr != nil {
				continue
			}
			signer, sigErr := hdKey.Signer()
			if sigErr != nil {
				continue
			}
			wset.signers[addr] = signer
		}

		for _, u := range utxos {
			// Skip immature coinbase outputs.
			if u.Coinbase && (currentHeight < u.Height || currentHeight-u.Height < config.CoinbaseMaturity) {
				if u.Token == nil {
					wset.immatureNative += u.Value
				}
				continue
			}
			// Skip locked outputs (e.g. unstake cooldown).
			if u.LockedUntil > 0 && currentHeight < u.LockedUntil {
				if u.Token == nil {
					wset.lockedNative += u.Value
				}
				continue
			}
			wset.utxos = append(wset.utxos, wallet.UTXO{
				Outpoint: u.Outpoint,
				Value:    u.Value,
				Script:   u.Script,
				Token:    u.Token,
			})
			wset.addrByOutpoint[u.Outpoint] = addr
			if u.Token == nil {
				wset.spendableNative += u.Value
			}
		}
	}

	return wset, nil
}

func filterNativeUTXOs(utxos []wallet.UTXO) []wallet.UTXO {
	native := make([]wallet.UTXO, 0, len(utxos))
	for _, u := range utxos {
		if u.Token == nil && u.Script.Type == types.ScriptTypeP2PKH {
			native = append(native, u)
		}
	}
	return native
}

// hasPendingStakeForPubKey reports whether any mempool tx currently creates
// a stake output for the given validator pubkey.
func hasPendingStakeForPubKey(txs []*tx.Transaction, pubKey []byte) bool {
	for _, transaction := range txs {
		for _, out := range transaction.Outputs {
			if out.Script.Type == types.ScriptTypeStake && bytes.Equal(out.Script.Data, pubKey) {
				return true
			}
		}
	}
	return false
}

// formatAmount converts raw base units to a human-readable decimal string.
func formatAmount(units uint64) string {
	whole := units / config.Coin
	frac := units % config.Coin
	return fmt.Sprintf("%d.%012d", whole, frac)
}

// requireWallet returns an error if the wallet keystore is not enabled.
func (s *Server) requireWallet() *Error {
	if s.keystore == nil {
		return &Error{Code: CodeInternalError, Message: "wallet not enabled (start node with --wallet)"}
	}
	return nil
}

func (s *Server) handleWalletCreate(req *Request) (interface{}, *Error) {
	if err := s.requireWallet(); err != nil {
		return nil, err
	}

	var params WalletCreateParam
	if err := parseParams(req, &params); err != nil {
		return nil, err
	}
	if params.Name == "" || params.Password == "" {
		return nil, &Error{Code: CodeInvalidParams, Message: "name and password are required"}
	}

	// Generate mnemonic.
	mnemonic, genErr := wallet.GenerateMnemonic()
	if genErr != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("generate mnemonic: %v", genErr)}
	}

	// Derive seed.
	seed, seedErr := wallet.SeedFromMnemonic(mnemonic, "")
	if seedErr != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("derive seed: %v", seedErr)}
	}

	// Derive account 0 address.
	master, masterErr := wallet.NewMasterKey(seed)
	if masterErr != nil {
		for i := range seed {
			seed[i] = 0
		}
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("derive master key: %v", masterErr)}
	}

	hdKey, derErr := master.DeriveAddress(0, wallet.ChangeExternal, 0)
	if derErr != nil {
		for i := range seed {
			seed[i] = 0
		}
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("derive address: %v", derErr)}
	}
	addr := hdKey.Address()

	// Create encrypted wallet.
	if err := s.keystore.Create(params.Name, seed, []byte(params.Password), wallet.DefaultParams()); err != nil {
		for i := range seed {
			seed[i] = 0
		}
		return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("create wallet: %v", err)}
	}

	// Zero seed.
	for i := range seed {
		seed[i] = 0
	}

	// Store account 0 metadata.
	if err := s.keystore.AddAccount(params.Name, wallet.AccountEntry{
		Index:   0,
		Name:    "Default",
		Address: addr.String(),
	}); err != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("add account: %v", err)}
	}

	return &WalletCreateResult{
		Mnemonic: mnemonic,
		Address:  addr.String(),
	}, nil
}

func (s *Server) handleWalletImport(req *Request) (interface{}, *Error) {
	if err := s.requireWallet(); err != nil {
		return nil, err
	}

	var params WalletImportParam
	if err := parseParams(req, &params); err != nil {
		return nil, err
	}
	// Normalize mnemonic: trim whitespace and collapse internal spaces/newlines.
	params.Mnemonic = strings.Join(strings.Fields(params.Mnemonic), " ")

	if params.Name == "" || params.Password == "" || params.Mnemonic == "" {
		return nil, &Error{Code: CodeInvalidParams, Message: "name, password, and mnemonic are required"}
	}

	if !wallet.ValidateMnemonic(params.Mnemonic) {
		return nil, &Error{Code: CodeInvalidParams, Message: "invalid mnemonic"}
	}

	// Derive seed.
	seed, seedErr := wallet.SeedFromMnemonic(params.Mnemonic, "")
	if seedErr != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("derive seed: %v", seedErr)}
	}

	// Derive account 0 address.
	master, masterErr := wallet.NewMasterKey(seed)
	if masterErr != nil {
		for i := range seed {
			seed[i] = 0
		}
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("derive master key: %v", masterErr)}
	}

	hdKey, derErr := master.DeriveAddress(0, wallet.ChangeExternal, 0)
	if derErr != nil {
		for i := range seed {
			seed[i] = 0
		}
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("derive address: %v", derErr)}
	}
	addr := hdKey.Address()

	// Create encrypted wallet.
	if err := s.keystore.Create(params.Name, seed, []byte(params.Password), wallet.DefaultParams()); err != nil {
		for i := range seed {
			seed[i] = 0
		}
		return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("create wallet: %v", err)}
	}

	// Zero seed.
	for i := range seed {
		seed[i] = 0
	}

	// Store account 0 metadata.
	if err := s.keystore.AddAccount(params.Name, wallet.AccountEntry{
		Index:   0,
		Name:    "Default",
		Address: addr.String(),
	}); err != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("add account: %v", err)}
	}

	// Scan for previously used addresses (gap limit discovery).
	s.scanWalletAddresses(params.Name, master)

	return &WalletImportResult{
		Address: addr.String(),
	}, nil
}

// scanWalletAddresses discovers previously used addresses via BIP-44 gap limit
// scanning and registers them in the wallet's account list. This allows reimported
// wallets to show their full balance across all derived addresses.
func (s *Server) scanWalletAddresses(walletName string, master *wallet.HDKey) {
	const gapLimit = 20

	// Scan external chain (change=0), then internal/change chain (change=1).
	for _, chain := range []struct {
		change     uint32
		namePrefix string
	}{
		{wallet.ChangeExternal, "Address"},
		{wallet.ChangeInternal, "Change"},
	} {
		var gap int
		var highestUsed int = -1

		for idx := uint32(0); gap < gapLimit; idx++ {
			hdKey, err := master.DeriveAddress(0, chain.change, idx)
			if err != nil {
				break
			}
			addr := hdKey.Address()

			utxos, err := s.utxos.GetByAddress(addr)
			hasUTXOs := err == nil && len(utxos) > 0

			// Also check stake UTXOs (indexed by pubkey, not address).
			if !hasUTXOs {
				stakes, sErr := stakesByAddress(s.utxos, addr)
				hasUTXOs = sErr == nil && len(stakes) > 0
			}

			if !hasUTXOs {
				gap++
				continue
			}

			// Address has UTXOs — register it.
			gap = 0
			highestUsed = int(idx)

			// Skip if already exists (e.g., account 0 added by handleWalletImport).
			_ = s.keystore.AddAccount(walletName, wallet.AccountEntry{
				Index:   idx,
				Change:  chain.change,
				Name:    fmt.Sprintf("%s %d", chain.namePrefix, idx),
				Address: addr.String(),
			})
		}

		// Set the next index to highestUsed + 1.
		if highestUsed >= 0 {
			nextIdx := uint32(highestUsed + 1)
			if chain.change == wallet.ChangeExternal {
				_ = s.keystore.SetExternalIndex(walletName, nextIdx)
			} else {
				_ = s.keystore.SetChangeIndex(walletName, nextIdx)
			}
		}
	}
}

func (s *Server) handleWalletList(_ *Request) (interface{}, *Error) {
	if err := s.requireWallet(); err != nil {
		return nil, err
	}

	names, listErr := s.keystore.List()
	if listErr != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("list wallets: %v", listErr)}
	}

	if names == nil {
		names = []string{}
	}

	return &WalletListResult{Wallets: names}, nil
}

func (s *Server) handleWalletNewAddress(req *Request) (interface{}, *Error) {
	if err := s.requireWallet(); err != nil {
		return nil, err
	}

	var params WalletNewAddressParam
	if err := parseParams(req, &params); err != nil {
		return nil, err
	}
	if params.Name == "" || params.Password == "" {
		return nil, &Error{Code: CodeInvalidParams, Message: "name and password are required"}
	}

	// Load seed.
	seed, loadErr := s.keystore.Load(params.Name, []byte(params.Password))
	if loadErr != nil {
		s.logger.Debug().Err(loadErr).Msg("wallet load failed")
		return nil, &Error{Code: CodeInvalidParams, Message: "invalid wallet name or password"}
	}

	master, masterErr := wallet.NewMasterKey(seed)
	for i := range seed {
		seed[i] = 0
	}
	if masterErr != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("derive master key: %v", masterErr)}
	}

	// Get current external index.
	extIdx, idxErr := s.keystore.GetExternalIndex(params.Name)
	if idxErr != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("get external index: %v", idxErr)}
	}

	// Use the next index (existing index 0 is already the default account).
	nextIdx := extIdx
	if nextIdx == 0 {
		nextIdx = 1 // Index 0 is already created at wallet creation time.
	}

	hdKey, derErr := master.DeriveAddress(0, wallet.ChangeExternal, nextIdx)
	if derErr != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("derive address: %v", derErr)}
	}
	addr := hdKey.Address()

	// Store account metadata.
	if err := s.keystore.AddAccount(params.Name, wallet.AccountEntry{
		Index:   nextIdx,
		Name:    fmt.Sprintf("Address %d", nextIdx),
		Address: addr.String(),
	}); err != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("add account: %v", err)}
	}

	// Advance external index.
	if err := s.keystore.IncrementExternalIndex(params.Name); err != nil {
		s.logger.Warn().Err(err).Msg("Failed to update external index")
	}

	return &WalletAddressResult{
		Index:   nextIdx,
		Address: addr.String(),
	}, nil
}

func (s *Server) handleWalletListAddresses(req *Request) (interface{}, *Error) {
	if err := s.requireWallet(); err != nil {
		return nil, err
	}

	var params WalletUnlockParam
	if err := parseParams(req, &params); err != nil {
		return nil, err
	}
	if params.Name == "" || params.Password == "" {
		return nil, &Error{Code: CodeInvalidParams, Message: "name and password are required"}
	}

	// Verify password by attempting to load.
	seed, loadErr := s.keystore.Load(params.Name, []byte(params.Password))
	if loadErr != nil {
		s.logger.Debug().Err(loadErr).Msg("wallet load failed")
		return nil, &Error{Code: CodeInvalidParams, Message: "invalid wallet name or password"}
	}
	for i := range seed {
		seed[i] = 0
	}

	accounts, accErr := s.keystore.ListAccounts(params.Name)
	if accErr != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("list accounts: %v", accErr)}
	}

	entries := make([]WalletAccountEntry, len(accounts))
	for i, a := range accounts {
		change, index := a.Derivation()
		entries[i] = WalletAccountEntry{
			Index:   index,
			Change:  change,
			Name:    a.Name,
			Address: a.Address,
		}
	}

	return &WalletAddressListResult{Accounts: entries}, nil
}

func (s *Server) handleWalletSend(req *Request) (interface{}, *Error) {
	if err := s.requireWallet(); err != nil {
		return nil, err
	}

	var params WalletSendParam
	if err := parseParams(req, &params); err != nil {
		return nil, err
	}
	if params.Name == "" || params.Password == "" || params.To == "" || params.Amount == 0 {
		return nil, &Error{Code: CodeInvalidParams, Message: "name, password, to, and amount are required"}
	}

	// Parse recipient address.
	recipientAddr, addrErr := decodeAddress(params.To)
	if addrErr != nil {
		return nil, addrErr
	}

	// Load wallet.
	seed, loadErr := s.keystore.Load(params.Name, []byte(params.Password))
	if loadErr != nil {
		s.logger.Debug().Err(loadErr).Msg("wallet load failed")
		return nil, &Error{Code: CodeInvalidParams, Message: "invalid wallet name or password"}
	}

	master, masterErr := wallet.NewMasterKey(seed)
	for i := range seed {
		seed[i] = 0
	}
	if masterErr != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("derive master key: %v", masterErr)}
	}

	// Collect UTXOs from all wallet addresses (external + change).
	wset, collectErr := s.collectWalletUTXOs(master, params.Name, s.utxos, s.chain.Height())
	if collectErr != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("collect utxos: %v", collectErr)}
	}
	defer wset.zeroSigners()
	nativeUTXOs := filterNativeUTXOs(wset.utxos)
	if len(nativeUTXOs) == 0 {
		return nil, &Error{
			Code: CodeInvalidParams,
			Message: fmt.Sprintf(
				"no spendable native UTXOs found for wallet (spendable=%d, immature=%d, locked=%d)",
				wset.spendableNative, wset.immatureNative, wset.lockedNative,
			),
		}
	}

	// Fee estimation with iterative coin selection.
	feeRate := s.genesis.Protocol.Consensus.MinFeeRate
	fee := tx.EstimateTxFee(1, 2, feeRate) // 1 input, 2 outputs (recipient + change)
	selection, selErr := wallet.SelectCoins(nativeUTXOs, params.Amount+fee)
	if selErr != nil {
		return nil, &Error{
			Code: CodeInvalidParams,
			Message: fmt.Sprintf(
				"coin selection: %v (spendable=%d, immature=%d, locked=%d, need=%d)",
				selErr, wset.spendableNative, wset.immatureNative, wset.lockedNative, params.Amount+fee,
			),
		}
	}
	// Recalculate fee with actual input count.
	fee = tx.EstimateTxFee(len(selection.Inputs), 2, feeRate)
	if selection.Total < params.Amount+fee {
		selection, selErr = wallet.SelectCoins(nativeUTXOs, params.Amount+fee)
		if selErr != nil {
			return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("coin selection: %v", selErr)}
		}
		fee = tx.EstimateTxFee(len(selection.Inputs), 2, feeRate)
	}
	change := selection.Total - params.Amount - fee

	// Build transaction.
	builder := tx.NewBuilder()
	for _, input := range selection.Inputs {
		builder.AddInput(input.Outpoint)
	}

	// Recipient output.
	recipientScript := types.Script{
		Type: types.ScriptTypeP2PKH,
		Data: recipientAddr.Bytes(),
	}
	builder.AddOutput(params.Amount, recipientScript)

	// Change output.
	var changeIdx uint32
	var changeAddr types.Address
	if change > 0 {
		var chErr error
		changeIdx, chErr = s.keystore.GetChangeIndex(params.Name)
		if chErr != nil {
			return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("get change index: %v", chErr)}
		}
		changeKey, chKeyErr := master.DeriveAddress(0, wallet.ChangeInternal, changeIdx)
		if chKeyErr != nil {
			return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("derive change address: %v", chKeyErr)}
		}
		changeAddr = changeKey.Address()
		changeScript := types.Script{
			Type: types.ScriptTypeP2PKH,
			Data: changeAddr.Bytes(),
		}
		builder.AddOutput(change, changeScript)
	}

	// Sign with per-input keys.
	if err := builder.SignMulti(wset.signers, wset.addrByOutpoint); err != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("sign transaction: %v", err)}
	}

	transaction := builder.Build()

	// Add to mempool.
	_, poolErr := s.pool.Add(transaction)
	if poolErr != nil {
		return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("rejected: %v", poolErr)}
	}

	// Broadcast to P2P network.
	if s.p2pNode != nil {
		if err := s.p2pNode.BroadcastTx(transaction); err != nil {
			s.logger.Warn().Err(err).Msg("Failed to broadcast transaction")
		}
	}

	// Track change address and advance index.
	if change > 0 {
		_ = s.keystore.AddAccount(params.Name, wallet.AccountEntry{
			Index:   changeIdx,
			Change:  wallet.ChangeInternal,
			Name:    fmt.Sprintf("Change %d", changeIdx),
			Address: changeAddr.String(),
		})
		if err := s.keystore.IncrementChangeIndex(params.Name); err != nil {
			s.logger.Warn().Err(err).Msg("Failed to update change index")
		}
	}

	return &WalletSendResult{
		TxHash: transaction.Hash().String(),
	}, nil
}

func (s *Server) handleWalletConsolidate(req *Request) (interface{}, *Error) {
	if err := s.requireWallet(); err != nil {
		return nil, err
	}

	var params WalletConsolidateParam
	if err := parseParams(req, &params); err != nil {
		return nil, err
	}
	if params.Name == "" || params.Password == "" {
		return nil, &Error{Code: CodeInvalidParams, Message: "name and password are required"}
	}

	maxInputs := params.MaxInputs
	if maxInputs == 0 {
		maxInputs = 500
	}
	if maxInputs > config.MaxTxInputs {
		return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("max_inputs too high: %d (max %d)", maxInputs, config.MaxTxInputs)}
	}
	if maxInputs < 2 {
		return nil, &Error{Code: CodeInvalidParams, Message: "max_inputs must be at least 2"}
	}

	// Resolve target chain context.
	store := utxoGetter(s.utxos)
	currentHeight := s.chain.Height()
	feeRate := s.genesis.Protocol.Consensus.MinFeeRate
	addToPool := func(t *tx.Transaction) error {
		_, err := s.pool.Add(t)
		return err
	}
	broadcast := func(t *tx.Transaction) {
		if s.p2pNode != nil {
			if err := s.p2pNode.BroadcastTx(t); err != nil {
				s.logger.Warn().Err(err).Msg("Failed to broadcast consolidation tx")
			}
		}
	}

	if params.ChainID != "" {
		if err := s.requireSubChainManager(); err != nil {
			return nil, err
		}

		chainIDBytes, decErr := hex.DecodeString(params.ChainID)
		if decErr != nil || len(chainIDBytes) != types.HashSize {
			return nil, &Error{Code: CodeInvalidParams, Message: "invalid chain_id: must be 32-byte hex"}
		}
		var chainID types.ChainID
		copy(chainID[:], chainIDBytes)

		sr, ok := s.scManager.GetChain(chainID)
		if !ok {
			return nil, &Error{Code: CodeNotFound, Message: fmt.Sprintf("sub-chain %s not synced on this node", params.ChainID)}
		}

		store = sr.UTXOs
		currentHeight = sr.Chain.Height()
		feeRate = sr.Genesis.Protocol.Consensus.MinFeeRate
		addToPool = func(t *tx.Transaction) error {
			_, err := sr.Pool.Add(t)
			return err
		}
		broadcast = func(t *tx.Transaction) {
			if s.p2pNode != nil {
				if err := s.p2pNode.BroadcastSubChainTx(params.ChainID, t); err != nil {
					s.logger.Warn().Err(err).Str("chain", params.ChainID).Msg("Failed to broadcast sub-chain consolidation tx")
				}
			}
		}
	}

	// Load wallet.
	seed, loadErr := s.keystore.Load(params.Name, []byte(params.Password))
	if loadErr != nil {
		s.logger.Debug().Err(loadErr).Msg("wallet load failed")
		return nil, &Error{Code: CodeInvalidParams, Message: "invalid wallet name or password"}
	}

	master, masterErr := wallet.NewMasterKey(seed)
	for i := range seed {
		seed[i] = 0
	}
	if masterErr != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("derive master key: %v", masterErr)}
	}

	// Collect spendable UTXOs from all wallet addresses.
	wset, collectErr := s.collectWalletUTXOs(master, params.Name, store, currentHeight)
	if collectErr != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("collect utxos: %v", collectErr)}
	}
	defer wset.zeroSigners()

	nativeUTXOs := filterNativeUTXOs(wset.utxos)
	if len(nativeUTXOs) < 2 {
		return nil, &Error{
			Code: CodeInvalidParams,
			Message: fmt.Sprintf(
				"not enough spendable native UTXOs to consolidate (count=%d, spendable=%d, immature=%d, locked=%d)",
				len(nativeUTXOs), wset.spendableNative, wset.immatureNative, wset.lockedNative,
			),
		}
	}

	// Consolidation prefers smallest UTXOs first.
	sort.Slice(nativeUTXOs, func(i, j int) bool {
		return nativeUTXOs[i].Value < nativeUTXOs[j].Value
	})

	limit := int(maxInputs)
	if limit > len(nativeUTXOs) {
		limit = len(nativeUTXOs)
	}
	if limit < 2 {
		return nil, &Error{Code: CodeInvalidParams, Message: "not enough UTXOs to consolidate"}
	}

	selected := nativeUTXOs[:limit]
	var total uint64
	for _, u := range selected {
		if total > ^uint64(0)-u.Value {
			return nil, &Error{Code: CodeInternalError, Message: "input value overflow"}
		}
		total += u.Value
	}
	fee := tx.EstimateTxFee(len(selected), 1, feeRate)
	if total <= fee {
		return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("selected UTXOs too small: total=%d, fee=%d", total, fee)}
	}

	// Consolidate into a single internal/change address.
	changeIdx, chErr := s.keystore.GetChangeIndex(params.Name)
	if chErr != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("get change index: %v", chErr)}
	}
	changeKey, chKeyErr := master.DeriveAddress(0, wallet.ChangeInternal, changeIdx)
	if chKeyErr != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("derive change address: %v", chKeyErr)}
	}
	changeAddr := changeKey.Address()
	changeScript := types.Script{
		Type: types.ScriptTypeP2PKH,
		Data: changeAddr.Bytes(),
	}

	builder := tx.NewBuilder()
	for _, input := range selected {
		builder.AddInput(input.Outpoint)
	}
	outputAmount := total - fee
	builder.AddOutput(outputAmount, changeScript)

	if err := builder.SignMulti(wset.signers, wset.addrByOutpoint); err != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("sign transaction: %v", err)}
	}

	transaction := builder.Build()
	if err := addToPool(transaction); err != nil {
		return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("rejected: %v", err)}
	}
	broadcast(transaction)

	// Track change address and advance index.
	_ = s.keystore.AddAccount(params.Name, wallet.AccountEntry{
		Index:   changeIdx,
		Change:  wallet.ChangeInternal,
		Name:    fmt.Sprintf("Change %d", changeIdx),
		Address: changeAddr.String(),
	})
	if err := s.keystore.IncrementChangeIndex(params.Name); err != nil {
		s.logger.Warn().Err(err).Msg("Failed to update change index")
	}

	return &WalletConsolidateResult{
		TxHash:       transaction.Hash().String(),
		ChainID:      params.ChainID,
		InputsUsed:   uint32(limit),
		InputTotal:   total,
		OutputAmount: outputAmount,
		Fee:          fee,
	}, nil
}

func (s *Server) handleWalletSendMany(req *Request) (interface{}, *Error) {
	if err := s.requireWallet(); err != nil {
		return nil, err
	}

	var params WalletSendManyParam
	if err := parseParams(req, &params); err != nil {
		return nil, err
	}
	if params.Name == "" || params.Password == "" {
		return nil, &Error{Code: CodeInvalidParams, Message: "name and password are required"}
	}
	if len(params.Recipients) == 0 {
		return nil, &Error{Code: CodeInvalidParams, Message: "at least one recipient is required"}
	}

	// Validate all recipients and compute total output amount.
	type parsed struct {
		addr   types.Address
		amount uint64
	}
	recipients := make([]parsed, len(params.Recipients))
	var totalAmount uint64
	for i, r := range params.Recipients {
		if r.To == "" || r.Amount == 0 {
			return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("recipient %d: to and amount are required", i)}
		}
		addr, addrErr := decodeAddress(r.To)
		if addrErr != nil {
			return nil, addrErr
		}
		recipients[i] = parsed{addr: addr, amount: r.Amount}
		totalAmount += r.Amount
	}

	// Load wallet.
	seed, loadErr := s.keystore.Load(params.Name, []byte(params.Password))
	if loadErr != nil {
		s.logger.Debug().Err(loadErr).Msg("wallet load failed")
		return nil, &Error{Code: CodeInvalidParams, Message: "invalid wallet name or password"}
	}

	master, masterErr := wallet.NewMasterKey(seed)
	for i := range seed {
		seed[i] = 0
	}
	if masterErr != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("derive master key: %v", masterErr)}
	}

	// Collect UTXOs from all wallet addresses.
	wset, collectErr := s.collectWalletUTXOs(master, params.Name, s.utxos, s.chain.Height())
	if collectErr != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("collect utxos: %v", collectErr)}
	}
	defer wset.zeroSigners()
	nativeUTXOs := filterNativeUTXOs(wset.utxos)
	if len(nativeUTXOs) == 0 {
		return nil, &Error{Code: CodeInvalidParams, Message: "no UTXOs found for wallet"}
	}

	// Fee estimation with iterative coin selection.
	feeRate := s.genesis.Protocol.Consensus.MinFeeRate
	numOutputs := len(recipients) + 1 // recipients + change
	fee := tx.EstimateTxFee(1, numOutputs, feeRate)
	selection, selErr := wallet.SelectCoins(nativeUTXOs, totalAmount+fee)
	if selErr != nil {
		return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("coin selection: %v", selErr)}
	}
	// Recalculate fee with actual input count.
	fee = tx.EstimateTxFee(len(selection.Inputs), numOutputs, feeRate)
	if selection.Total < totalAmount+fee {
		selection, selErr = wallet.SelectCoins(nativeUTXOs, totalAmount+fee)
		if selErr != nil {
			return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("coin selection: %v", selErr)}
		}
		fee = tx.EstimateTxFee(len(selection.Inputs), numOutputs, feeRate)
	}
	change := selection.Total - totalAmount - fee

	// Build transaction.
	builder := tx.NewBuilder()
	for _, input := range selection.Inputs {
		builder.AddInput(input.Outpoint)
	}

	// Add all recipient outputs.
	for _, r := range recipients {
		script := types.Script{
			Type: types.ScriptTypeP2PKH,
			Data: r.addr.Bytes(),
		}
		builder.AddOutput(r.amount, script)
	}

	// Change output.
	var changeIdx uint32
	var changeAddr types.Address
	if change > 0 {
		var chErr error
		changeIdx, chErr = s.keystore.GetChangeIndex(params.Name)
		if chErr != nil {
			return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("get change index: %v", chErr)}
		}
		changeKey, chKeyErr := master.DeriveAddress(0, wallet.ChangeInternal, changeIdx)
		if chKeyErr != nil {
			return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("derive change address: %v", chKeyErr)}
		}
		changeAddr = changeKey.Address()
		changeScript := types.Script{
			Type: types.ScriptTypeP2PKH,
			Data: changeAddr.Bytes(),
		}
		builder.AddOutput(change, changeScript)
	}

	// Sign with per-input keys.
	if err := builder.SignMulti(wset.signers, wset.addrByOutpoint); err != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("sign transaction: %v", err)}
	}

	transaction := builder.Build()

	// Add to mempool.
	_, poolErr := s.pool.Add(transaction)
	if poolErr != nil {
		return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("rejected: %v", poolErr)}
	}

	// Broadcast to P2P network.
	if s.p2pNode != nil {
		if err := s.p2pNode.BroadcastTx(transaction); err != nil {
			s.logger.Warn().Err(err).Msg("Failed to broadcast transaction")
		}
	}

	// Track change address and advance index.
	if change > 0 {
		_ = s.keystore.AddAccount(params.Name, wallet.AccountEntry{
			Index:   changeIdx,
			Change:  wallet.ChangeInternal,
			Name:    fmt.Sprintf("Change %d", changeIdx),
			Address: changeAddr.String(),
		})
		if err := s.keystore.IncrementChangeIndex(params.Name); err != nil {
			s.logger.Warn().Err(err).Msg("Failed to update change index")
		}
	}

	return &WalletSendManyResult{
		TxHash: transaction.Hash().String(),
	}, nil
}

func (s *Server) handleWalletExportKey(req *Request) (interface{}, *Error) {
	if err := s.requireWallet(); err != nil {
		return nil, err
	}

	var params WalletExportKeyParam
	if err := parseParams(req, &params); err != nil {
		return nil, err
	}
	if params.Name == "" || params.Password == "" {
		return nil, &Error{Code: CodeInvalidParams, Message: "name and password are required"}
	}

	// Load seed.
	seed, loadErr := s.keystore.Load(params.Name, []byte(params.Password))
	if loadErr != nil {
		s.logger.Debug().Err(loadErr).Msg("wallet load failed")
		return nil, &Error{Code: CodeInvalidParams, Message: "invalid wallet name or password"}
	}

	master, masterErr := wallet.NewMasterKey(seed)
	for i := range seed {
		seed[i] = 0
	}
	if masterErr != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("derive master key: %v", masterErr)}
	}

	hdKey, derErr := master.DeriveAddress(params.Account, wallet.ChangeExternal, params.Index)
	if derErr != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("derive key: %v", derErr)}
	}

	privBytes := hdKey.PrivateKeyBytes()
	if privBytes == nil {
		return nil, &Error{Code: CodeInternalError, Message: "no private key available"}
	}

	pubBytes := hdKey.PublicKeyBytes()
	addr := hdKey.Address()

	privHexBytes := []byte(hex.EncodeToString(privBytes))

	// Zero private key bytes.
	for i := range privBytes {
		privBytes[i] = 0
	}

	result := &WalletExportKeyResult{
		PrivateKey: string(privHexBytes),
		PubKey:     hex.EncodeToString(pubBytes),
		Address:    addr.String(),
	}

	// Best-effort zero of hex bytes (Go strings are immutable copies).
	for i := range privHexBytes {
		privHexBytes[i] = 0
	}

	return result, nil
}

func (s *Server) handleWalletStake(req *Request) (interface{}, *Error) {
	if err := s.requireWallet(); err != nil {
		return nil, err
	}

	var params WalletStakeParam
	if err := parseParams(req, &params); err != nil {
		return nil, err
	}
	if params.Name == "" || params.Password == "" {
		return nil, &Error{Code: CodeInvalidParams, Message: "name and password are required"}
	}
	if params.Amount == 0 {
		return nil, &Error{Code: CodeInvalidParams, Message: "amount must be positive"}
	}

	// Validate amount == validator stake (exact match required).
	requiredStake := s.genesis.Protocol.Consensus.ValidatorStake
	if requiredStake > 0 && params.Amount != requiredStake {
		return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("stake must be exactly %d, got %d", requiredStake, params.Amount)}
	}

	// Load wallet.
	seed, loadErr := s.keystore.Load(params.Name, []byte(params.Password))
	if loadErr != nil {
		s.logger.Debug().Err(loadErr).Msg("wallet load failed")
		return nil, &Error{Code: CodeInvalidParams, Message: "invalid wallet name or password"}
	}

	master, masterErr := wallet.NewMasterKey(seed)
	for i := range seed {
		seed[i] = 0
	}
	if masterErr != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("derive master key: %v", masterErr)}
	}

	// Derive account 0 for the stake pubkey (staking always uses account 0's pubkey).
	hdKey, derErr := master.DeriveAddress(0, wallet.ChangeExternal, 0)
	if derErr != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("derive address key: %v", derErr)}
	}
	pubKeyBytes := hdKey.PublicKeyBytes()

	// Block duplicate active/pending stake for this validator pubkey.
	existingStakes, stakeErr := s.utxos.GetStakes(pubKeyBytes)
	if stakeErr != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("get existing stakes: %v", stakeErr)}
	}
	if len(existingStakes) > 0 || hasPendingStakeForPubKey(s.pool.SelectForBlock(s.pool.Count()), pubKeyBytes) {
		return nil, &Error{Code: CodeInvalidParams, Message: "validator already has an active or pending stake; unstake before staking again"}
	}

	// Collect UTXOs from all wallet addresses (external + change).
	wset, collectErr := s.collectWalletUTXOs(master, params.Name, s.utxos, s.chain.Height())
	if collectErr != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("collect utxos: %v", collectErr)}
	}
	defer wset.zeroSigners()
	nativeUTXOs := filterNativeUTXOs(wset.utxos)
	if len(nativeUTXOs) == 0 {
		return nil, &Error{Code: CodeInvalidParams, Message: "no UTXOs found for wallet"}
	}

	// Fee estimation with iterative coin selection.
	feeRate := s.genesis.Protocol.Consensus.MinFeeRate
	fee := tx.EstimateTxFee(1, 2, feeRate) // 1 input, 2 outputs (stake + change)
	selection, selErr := wallet.SelectCoins(nativeUTXOs, params.Amount+fee)
	if selErr != nil {
		return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("coin selection: %v", selErr)}
	}
	// Recalculate fee with actual input count.
	fee = tx.EstimateTxFee(len(selection.Inputs), 2, feeRate)
	if selection.Total < params.Amount+fee {
		selection, selErr = wallet.SelectCoins(nativeUTXOs, params.Amount+fee)
		if selErr != nil {
			return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("coin selection: %v", selErr)}
		}
		fee = tx.EstimateTxFee(len(selection.Inputs), 2, feeRate)
	}
	change := selection.Total - params.Amount - fee

	// Stake output: ScriptTypeStake, data = 33-byte compressed pubkey.
	stakeScript := types.Script{
		Type: types.ScriptTypeStake,
		Data: pubKeyBytes,
	}

	// Get change index for the change output.
	var changeIdx uint32
	if change > 0 {
		var chErr error
		changeIdx, chErr = s.keystore.GetChangeIndex(params.Name)
		if chErr != nil {
			return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("get change index: %v", chErr)}
		}
	}

	// Build, check exact fee, and rebuild if needed.
	buildStakeTx := func(ch uint64) *tx.Transaction {
		b := tx.NewBuilder()
		for _, input := range selection.Inputs {
			b.AddInput(input.Outpoint)
		}
		b.AddOutput(params.Amount, stakeScript)
		if ch > 0 {
			chKey, _ := master.DeriveAddress(0, wallet.ChangeInternal, changeIdx)
			b.AddOutput(ch, types.Script{
				Type: types.ScriptTypeP2PKH,
				Data: chKey.Address().Bytes(),
			})
		}
		b.SignMulti(wset.signers, wset.addrByOutpoint)
		return b.Build()
	}

	transaction := buildStakeTx(change)
	exactFee := tx.RequiredFee(transaction, feeRate)
	if fee < exactFee {
		if change >= exactFee-fee {
			change -= exactFee - fee
			transaction = buildStakeTx(change)
		} else {
			// Re-select with exact fee.
			selection, selErr = wallet.SelectCoins(nativeUTXOs, params.Amount+exactFee)
			if selErr != nil {
				return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("coin selection: %v", selErr)}
			}
			change = selection.Total - params.Amount - exactFee
			transaction = buildStakeTx(change)
		}
	}

	// Add to mempool.
	_, poolErr := s.pool.Add(transaction)
	if poolErr != nil {
		return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("rejected: %v", poolErr)}
	}

	// Broadcast to P2P network.
	if s.p2pNode != nil {
		if err := s.p2pNode.BroadcastTx(transaction); err != nil {
			s.logger.Warn().Err(err).Msg("Failed to broadcast stake transaction")
		}
	}

	// Track change address and advance index.
	if change > 0 {
		changeKey, _ := master.DeriveAddress(0, wallet.ChangeInternal, changeIdx)
		changeAddr := changeKey.Address()
		_ = s.keystore.AddAccount(params.Name, wallet.AccountEntry{
			Index:   changeIdx,
			Change:  wallet.ChangeInternal,
			Name:    fmt.Sprintf("Change %d", changeIdx),
			Address: changeAddr.String(),
		})
		if err := s.keystore.IncrementChangeIndex(params.Name); err != nil {
			s.logger.Warn().Err(err).Msg("Failed to update change index")
		}
	}

	return &WalletStakeResult{
		TxHash: transaction.Hash().String(),
		PubKey: hex.EncodeToString(pubKeyBytes),
	}, nil
}

func (s *Server) handleWalletMintToken(req *Request) (interface{}, *Error) {
	if err := s.requireWallet(); err != nil {
		return nil, err
	}

	var params WalletMintTokenParam
	if err := parseParams(req, &params); err != nil {
		return nil, err
	}
	if params.Name == "" || params.Password == "" {
		return nil, &Error{Code: CodeInvalidParams, Message: "name and password are required"}
	}
	if params.TokenName == "" || params.Symbol == "" {
		return nil, &Error{Code: CodeInvalidParams, Message: "token_name and token_symbol are required"}
	}
	if params.Amount == 0 {
		return nil, &Error{Code: CodeInvalidParams, Message: "amount must be positive"}
	}
	if params.Amount > config.MaxTokenAmount {
		return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("amount exceeds maximum (%d)", config.MaxTokenAmount)}
	}
	// Validate token metadata.
	if !tokenNamePattern.MatchString(params.TokenName) {
		return nil, &Error{Code: CodeInvalidParams, Message: "token_name must be 1-64 alphanumeric/space/hyphen characters"}
	}
	if !tokenSymbolPattern.MatchString(params.Symbol) {
		return nil, &Error{Code: CodeInvalidParams, Message: "token_symbol must be 2-10 uppercase alphanumeric characters"}
	}
	if params.Decimals > 18 {
		return nil, &Error{Code: CodeInvalidParams, Message: "decimals must be 0-18"}
	}

	// Load wallet.
	seed, loadErr := s.keystore.Load(params.Name, []byte(params.Password))
	if loadErr != nil {
		s.logger.Debug().Err(loadErr).Msg("wallet load failed")
		return nil, &Error{Code: CodeInvalidParams, Message: "invalid wallet name or password"}
	}

	master, masterErr := wallet.NewMasterKey(seed)
	for i := range seed {
		seed[i] = 0
	}
	if masterErr != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("derive master key: %v", masterErr)}
	}

	// Derive account 0 for sender address (used as default recipient and token creator).
	hdKey, derErr := master.DeriveAddress(0, wallet.ChangeExternal, 0)
	if derErr != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("derive address key: %v", derErr)}
	}
	senderAddr := hdKey.Address()

	// Determine recipient (defaults to sender).
	recipientAddr := senderAddr
	if params.Recipient != "" {
		parsed, addrErr := decodeAddress(params.Recipient)
		if addrErr != nil {
			return nil, addrErr
		}
		recipientAddr = parsed
	}

	// Collect UTXOs from all wallet addresses (external + change).
	wset, collectErr := s.collectWalletUTXOs(master, params.Name, s.utxos, s.chain.Height())
	if collectErr != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("collect utxos: %v", collectErr)}
	}
	defer wset.zeroSigners()
	nativeUTXOs := filterNativeUTXOs(wset.utxos)
	if len(nativeUTXOs) == 0 {
		return nil, &Error{Code: CodeInvalidParams, Message: "no UTXOs found for wallet"}
	}

	// The fee must cover both the token creation fee and the per-byte tx fee.
	feeRate := s.genesis.Protocol.Consensus.MinFeeRate
	rateFee := tx.EstimateTxFee(1, 2, feeRate) // 1 input, 2 outputs (mint + change)
	burnFee := uint64(config.TokenCreationFee)
	target := burnFee
	if rateFee > target {
		target = rateFee
	}

	// Coin selection.
	selection, selErr := wallet.SelectCoins(nativeUTXOs, target)
	if selErr != nil {
		return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("coin selection: %v (need %d for token creation fee)", selErr, target)}
	}
	// Recalculate fee with actual input count.
	rateFee = tx.EstimateTxFee(len(selection.Inputs), 2, feeRate)
	target = burnFee
	if rateFee > target {
		target = rateFee
	}
	if selection.Total < target {
		selection, selErr = wallet.SelectCoins(nativeUTXOs, target)
		if selErr != nil {
			return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("coin selection: %v (need %d for token creation fee)", selErr, target)}
		}
		rateFee = tx.EstimateTxFee(len(selection.Inputs), 2, feeRate)
		if rateFee > burnFee {
			target = rateFee
		} else {
			target = burnFee
		}
	}
	change := selection.Total - target

	// Build transaction.
	builder := tx.NewBuilder()
	for _, input := range selection.Inputs {
		builder.AddInput(input.Outpoint)
	}

	// Derive token ID from the first input.
	firstInput := selection.Inputs[0].Outpoint
	tokenID := token.DeriveTokenID(firstInput.TxID, firstInput.Index)

	// Mint output: ScriptTypeMint, value=0, carries token data.
	mintScript := types.Script{
		Type: types.ScriptTypeMint,
		Data: recipientAddr.Bytes(),
	}
	builder.AddTokenOutput(0, mintScript, types.TokenData{
		ID:     tokenID,
		Amount: params.Amount,
	})

	// Change output.
	var changeIdx uint32
	if change > 0 {
		var chErr error
		changeIdx, chErr = s.keystore.GetChangeIndex(params.Name)
		if chErr != nil {
			return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("get change index: %v", chErr)}
		}
		changeKey, chKeyErr := master.DeriveAddress(0, wallet.ChangeInternal, changeIdx)
		if chKeyErr != nil {
			return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("derive change address: %v", chKeyErr)}
		}
		changeScript := types.Script{
			Type: types.ScriptTypeP2PKH,
			Data: changeKey.Address().Bytes(),
		}
		builder.AddOutput(change, changeScript)
	}

	// Sign with per-input keys.
	if err := builder.SignMulti(wset.signers, wset.addrByOutpoint); err != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("sign transaction: %v", err)}
	}

	transaction := builder.Build()

	// Add to mempool.
	_, poolErr := s.pool.Add(transaction)
	if poolErr != nil {
		return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("rejected: %v", poolErr)}
	}

	// Broadcast to P2P network.
	if s.p2pNode != nil {
		if err := s.p2pNode.BroadcastTx(transaction); err != nil {
			s.logger.Warn().Err(err).Msg("Failed to broadcast mint transaction")
		}
	}

	// Track change address and advance index.
	if change > 0 {
		changeKey, _ := master.DeriveAddress(0, wallet.ChangeInternal, changeIdx)
		changeAddr := changeKey.Address()
		_ = s.keystore.AddAccount(params.Name, wallet.AccountEntry{
			Index:   changeIdx,
			Change:  wallet.ChangeInternal,
			Name:    fmt.Sprintf("Change %d", changeIdx),
			Address: changeAddr.String(),
		})
		if err := s.keystore.IncrementChangeIndex(params.Name); err != nil {
			s.logger.Warn().Err(err).Msg("Failed to update change index")
		}
	}

	// Persist token metadata if token store is available.
	if s.tokenStore != nil {
		_ = s.tokenStore.Put(tokenID, &token.Metadata{
			Name:     params.TokenName,
			Symbol:   params.Symbol,
			Decimals: params.Decimals,
			Creator:  senderAddr,
		})
	}

	return &WalletMintTokenResult{
		TxHash:  transaction.Hash().String(),
		TokenID: hex.EncodeToString(tokenID[:]),
	}, nil
}

func (s *Server) handleWalletUnstake(req *Request) (interface{}, *Error) {
	if err := s.requireWallet(); err != nil {
		return nil, err
	}

	var params WalletUnstakeParam
	if err := parseParams(req, &params); err != nil {
		return nil, err
	}
	if params.Name == "" || params.Password == "" {
		return nil, &Error{Code: CodeInvalidParams, Message: "name and password are required"}
	}

	// Load wallet.
	seed, loadErr := s.keystore.Load(params.Name, []byte(params.Password))
	if loadErr != nil {
		s.logger.Debug().Err(loadErr).Msg("wallet load failed")
		return nil, &Error{Code: CodeInvalidParams, Message: "invalid wallet name or password"}
	}

	master, masterErr := wallet.NewMasterKey(seed)
	for i := range seed {
		seed[i] = 0
	}
	if masterErr != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("derive master key: %v", masterErr)}
	}

	hdKey, derErr := master.DeriveAddress(0, wallet.ChangeExternal, 0)
	if derErr != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("derive address key: %v", derErr)}
	}

	signer, sigErr := hdKey.Signer()
	if sigErr != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("get signer: %v", sigErr)}
	}
	defer signer.Zero()
	senderAddr := hdKey.Address()
	pubKeyBytes := hdKey.PublicKeyBytes()

	// Fetch all stake UTXOs for this pubkey.
	stakes, stakeErr := s.utxos.GetStakes(pubKeyBytes)
	if stakeErr != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("get stakes: %v", stakeErr)}
	}
	if len(stakes) == 0 {
		return nil, &Error{Code: CodeInvalidParams, Message: "no active stakes found for this wallet"}
	}

	// Sum total staked.
	var totalStaked uint64
	for _, st := range stakes {
		totalStaked += st.Value
	}

	// Fee estimation from genesis.
	feeRate := s.genesis.Protocol.Consensus.MinFeeRate
	fee := tx.EstimateTxFee(len(stakes), 1, feeRate)
	if totalStaked <= fee {
		return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("staked amount %d too small to cover fee %d", totalStaked, fee)}
	}

	// Build transaction: inputs = all stake UTXOs, output = P2PKH to sender.
	builder := tx.NewBuilder()

	// Build signers/outpoint maps for SignMulti (stake UTXOs are all owned by account 0).
	signers := map[types.Address]*crypto.PrivateKey{senderAddr: signer}
	outpointAddr := make(map[types.Outpoint]types.Address, len(stakes))
	for _, st := range stakes {
		builder.AddInput(st.Outpoint)
		outpointAddr[st.Outpoint] = senderAddr
	}

	// Return output: total staked minus fee.
	returnScript := types.Script{
		Type: types.ScriptTypeP2PKH,
		Data: senderAddr.Bytes(),
	}
	builder.AddOutput(totalStaked-fee, returnScript)

	// Sign with per-input keys.
	if err := builder.SignMulti(signers, outpointAddr); err != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("sign transaction: %v", err)}
	}

	transaction := builder.Build()

	// Add to mempool.
	_, poolErr := s.pool.Add(transaction)
	if poolErr != nil {
		return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("rejected: %v", poolErr)}
	}

	// Broadcast to P2P network.
	if s.p2pNode != nil {
		if err := s.p2pNode.BroadcastTx(transaction); err != nil {
			s.logger.Warn().Err(err).Msg("Failed to broadcast unstake transaction")
		}
	}

	return &WalletUnstakeResult{
		TxHash: transaction.Hash().String(),
		Amount: totalStaked,
		PubKey: hex.EncodeToString(pubKeyBytes),
	}, nil
}

func (s *Server) handleWalletSendToken(req *Request) (interface{}, *Error) {
	if err := s.requireWallet(); err != nil {
		return nil, err
	}

	var params WalletSendTokenParam
	if err := parseParams(req, &params); err != nil {
		return nil, err
	}
	if params.Name == "" || params.Password == "" || params.TokenID == "" || params.To == "" || params.Amount == 0 {
		return nil, &Error{Code: CodeInvalidParams, Message: "name, password, token_id, to, and amount are required"}
	}

	// Parse token ID.
	tokenIDBytes, decErr := hex.DecodeString(params.TokenID)
	if decErr != nil || len(tokenIDBytes) != types.HashSize {
		return nil, &Error{Code: CodeInvalidParams, Message: "invalid token_id: must be 32-byte hex"}
	}
	var tokenID types.TokenID
	copy(tokenID[:], tokenIDBytes)

	// Parse recipient address.
	recipientAddr, addrErr := decodeAddress(params.To)
	if addrErr != nil {
		return nil, addrErr
	}

	// Load wallet.
	seed, loadErr := s.keystore.Load(params.Name, []byte(params.Password))
	if loadErr != nil {
		s.logger.Debug().Err(loadErr).Msg("wallet load failed")
		return nil, &Error{Code: CodeInvalidParams, Message: "invalid wallet name or password"}
	}

	master, masterErr := wallet.NewMasterKey(seed)
	for i := range seed {
		seed[i] = 0
	}
	if masterErr != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("derive master key: %v", masterErr)}
	}

	// Derive account 0 for sender address (used for token change output).
	hdKey0, derErr := master.DeriveAddress(0, wallet.ChangeExternal, 0)
	if derErr != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("derive address key: %v", derErr)}
	}
	senderAddr := hdKey0.Address()

	// Collect UTXOs from all wallet addresses (external + change).
	wset, collectErr := s.collectWalletUTXOs(master, params.Name, s.utxos, s.chain.Height())
	if collectErr != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("collect utxos: %v", collectErr)}
	}
	defer wset.zeroSigners()

	// Separate token UTXOs (matching token ID) from KGX UTXOs.
	var tokenUTXOs []wallet.UTXO
	var kgxUTXOs []wallet.UTXO
	for _, u := range wset.utxos {
		if u.Token != nil && u.Token.ID == tokenID {
			tokenUTXOs = append(tokenUTXOs, u)
		} else if u.Token == nil && u.Script.Type == types.ScriptTypeP2PKH {
			kgxUTXOs = append(kgxUTXOs, u)
		}
	}

	if len(tokenUTXOs) == 0 {
		return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("no token UTXOs found for token %s", params.TokenID)}
	}

	// Select token UTXOs until we have enough.
	var selectedTokenUTXOs []wallet.UTXO
	var tokenSum uint64
	for _, u := range tokenUTXOs {
		selectedTokenUTXOs = append(selectedTokenUTXOs, u)
		tokenSum += u.Token.Amount
		if tokenSum >= params.Amount {
			break
		}
	}
	if tokenSum < params.Amount {
		return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("insufficient token balance: have %d, need %d", tokenSum, params.Amount)}
	}

	// Select KGX UTXOs to cover the per-byte fee.
	feeRate := s.genesis.Protocol.Consensus.MinFeeRate
	// Estimate outputs: token recipient + possible token change + possible KGX change = up to 3.
	numTokenOutputs := 1
	if tokenSum > params.Amount {
		numTokenOutputs = 2 // token recipient + token change
	}
	numInputsEst := len(selectedTokenUTXOs) + 1 // token inputs + at least 1 KGX input
	fee := tx.EstimateTxFee(numInputsEst, numTokenOutputs+1, feeRate) // +1 for KGX change
	kgxSelection, selErr := wallet.SelectCoins(kgxUTXOs, fee)
	if selErr == nil {
		// Recalculate with actual KGX input count.
		totalInputs := len(selectedTokenUTXOs) + len(kgxSelection.Inputs)
		fee = tx.EstimateTxFee(totalInputs, numTokenOutputs+1, feeRate)
		if kgxSelection.Total < fee {
			kgxSelection, selErr = wallet.SelectCoins(kgxUTXOs, fee)
			if selErr == nil {
				totalInputs = len(selectedTokenUTXOs) + len(kgxSelection.Inputs)
				fee = tx.EstimateTxFee(totalInputs, numTokenOutputs+1, feeRate)
			}
		}
	}
	if selErr != nil {
		return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("coin selection for fee: %v", selErr)}
	}

	// Token change amount.
	tokenChange := tokenSum - params.Amount

	// Get change index for the KGX change output.
	kgxChange := kgxSelection.Total - fee
	var changeIdx uint32
	if kgxChange > 0 {
		var chErr error
		changeIdx, chErr = s.keystore.GetChangeIndex(params.Name)
		if chErr != nil {
			return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("get change index: %v", chErr)}
		}
	}

	// Build, check exact fee, and rebuild if needed.
	buildTokenTx := func(ch uint64) *tx.Transaction {
		b := tx.NewBuilder()
		for _, u := range selectedTokenUTXOs {
			b.AddInput(u.Outpoint)
		}
		for _, u := range kgxSelection.Inputs {
			b.AddInput(u.Outpoint)
		}
		b.AddTokenOutput(0, types.Script{Type: types.ScriptTypeP2PKH, Data: recipientAddr.Bytes()}, types.TokenData{ID: tokenID, Amount: params.Amount})
		if tokenChange > 0 {
			b.AddTokenOutput(0, types.Script{Type: types.ScriptTypeP2PKH, Data: senderAddr.Bytes()}, types.TokenData{ID: tokenID, Amount: tokenChange})
		}
		if ch > 0 {
			chKey, _ := master.DeriveAddress(0, wallet.ChangeInternal, changeIdx)
			b.AddOutput(ch, types.Script{Type: types.ScriptTypeP2PKH, Data: chKey.Address().Bytes()})
		}
		b.SignMulti(wset.signers, wset.addrByOutpoint)
		return b.Build()
	}

	transaction := buildTokenTx(kgxChange)
	exactFee := tx.RequiredFee(transaction, feeRate)
	if fee < exactFee {
		if kgxChange >= exactFee-fee {
			kgxChange -= exactFee - fee
			transaction = buildTokenTx(kgxChange)
		} else {
			// Re-select KGX UTXOs with exact fee.
			kgxSelection, selErr = wallet.SelectCoins(kgxUTXOs, exactFee)
			if selErr != nil {
				return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("coin selection for fee: %v", selErr)}
			}
			kgxChange = kgxSelection.Total - exactFee
			transaction = buildTokenTx(kgxChange)
		}
	}

	// Add to mempool.
	_, poolErr := s.pool.Add(transaction)
	if poolErr != nil {
		return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("rejected: %v", poolErr)}
	}

	// Broadcast to P2P network.
	if s.p2pNode != nil {
		if err := s.p2pNode.BroadcastTx(transaction); err != nil {
			s.logger.Warn().Err(err).Msg("Failed to broadcast token transfer")
		}
	}

	// Track KGX change address and advance index.
	if kgxChange > 0 {
		changeKey, _ := master.DeriveAddress(0, wallet.ChangeInternal, changeIdx)
		changeAddr := changeKey.Address()
		_ = s.keystore.AddAccount(params.Name, wallet.AccountEntry{
			Index:   changeIdx,
			Change:  wallet.ChangeInternal,
			Name:    fmt.Sprintf("Change %d", changeIdx),
			Address: changeAddr.String(),
		})
		if err := s.keystore.IncrementChangeIndex(params.Name); err != nil {
			s.logger.Warn().Err(err).Msg("Failed to update change index")
		}
	}

	return &WalletSendTokenResult{
		TxHash: transaction.Hash().String(),
	}, nil
}

func (s *Server) handleWalletCreateSubChain(req *Request) (interface{}, *Error) {
	if err := s.requireWallet(); err != nil {
		return nil, err
	}
	if err := s.requireSubChainManager(); err != nil {
		return nil, err
	}

	var params WalletCreateSubChainParam
	if err := parseParams(req, &params); err != nil {
		return nil, err
	}
	if params.Name == "" || params.Password == "" {
		return nil, &Error{Code: CodeInvalidParams, Message: "name and password are required"}
	}
	if params.ChainName == "" || params.Symbol == "" || params.ConsensusType == "" {
		return nil, &Error{Code: CodeInvalidParams, Message: "chain_name, symbol, and consensus_type are required"}
	}

	// Burn amount is the fixed protocol constant (not user-configurable).
	burnAmount := s.genesis.Protocol.SubChain.MinDeposit

	// Build registration data.
	rd := subchain.RegistrationData{
		Name:              params.ChainName,
		Symbol:            params.Symbol,
		ConsensusType:     params.ConsensusType,
		BlockTime:         params.BlockTime,
		BlockReward:       params.BlockReward,
		MaxSupply:         params.MaxSupply,
		MinFeeRate:        params.MinFeeRate,
		Validators:        params.Validators,
		InitialDifficulty: params.InitialDifficulty,
		DifficultyAdjust:  params.DifficultyAdjust,
		ValidatorStake:    params.ValidatorStake,
	}

	// Validate registration data BEFORE building the tx.
	// This prevents burning funds for a registration that will be rejected.
	if err := subchain.ValidateRegistrationData(&rd, &s.genesis.Protocol.SubChain); err != nil {
		return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("invalid registration: %v", err)}
	}

	// Serialize to JSON for the script data.
	rdJSON, jsonErr := json.Marshal(rd)
	if jsonErr != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("marshal registration data: %v", jsonErr)}
	}

	// Load wallet.
	seed, loadErr := s.keystore.Load(params.Name, []byte(params.Password))
	if loadErr != nil {
		s.logger.Debug().Err(loadErr).Msg("wallet load failed")
		return nil, &Error{Code: CodeInvalidParams, Message: "invalid wallet name or password"}
	}

	master, masterErr := wallet.NewMasterKey(seed)
	for i := range seed {
		seed[i] = 0
	}
	if masterErr != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("derive master key: %v", masterErr)}
	}

	// Collect UTXOs from all wallet addresses (external + change).
	wset, collectErr := s.collectWalletUTXOs(master, params.Name, s.utxos, s.chain.Height())
	if collectErr != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("collect utxos: %v", collectErr)}
	}
	defer wset.zeroSigners()
	nativeUTXOs := filterNativeUTXOs(wset.utxos)
	if len(nativeUTXOs) == 0 {
		return nil, &Error{Code: CodeInvalidParams, Message: "no UTXOs found for wallet"}
	}

	// Fee estimation with iterative coin selection.
	feeRate := s.genesis.Protocol.Consensus.MinFeeRate
	fee := tx.EstimateTxFee(1, 2, feeRate) // 1 input, 2 outputs (register + change)
	selection, selErr := wallet.SelectCoins(nativeUTXOs, burnAmount+fee)
	if selErr != nil {
		return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("coin selection: %v (need burn + fee)", selErr)}
	}
	// Recalculate fee with actual input count.
	fee = tx.EstimateTxFee(len(selection.Inputs), 2, feeRate)
	if selection.Total < burnAmount+fee {
		selection, selErr = wallet.SelectCoins(nativeUTXOs, burnAmount+fee)
		if selErr != nil {
			return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("coin selection: %v (need burn + fee)", selErr)}
		}
		fee = tx.EstimateTxFee(len(selection.Inputs), 2, feeRate)
	}
	change := selection.Total - burnAmount - fee

	// Registration output (burned — unspendable ScriptTypeRegister).
	registerScript := types.Script{
		Type: types.ScriptTypeRegister,
		Data: rdJSON,
	}

	// Get change index for the change output.
	var changeIdx uint32
	if change > 0 {
		var chErr error
		changeIdx, chErr = s.keystore.GetChangeIndex(params.Name)
		if chErr != nil {
			return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("get change index: %v", chErr)}
		}
	}

	// Build, check exact fee, and rebuild if needed.
	buildRegTx := func(ch uint64) *tx.Transaction {
		b := tx.NewBuilder()
		for _, input := range selection.Inputs {
			b.AddInput(input.Outpoint)
		}
		b.AddOutput(burnAmount, registerScript)
		if ch > 0 {
			chKey, _ := master.DeriveAddress(0, wallet.ChangeInternal, changeIdx)
			b.AddOutput(ch, types.Script{
				Type: types.ScriptTypeP2PKH,
				Data: chKey.Address().Bytes(),
			})
		}
		b.SignMulti(wset.signers, wset.addrByOutpoint)
		return b.Build()
	}

	transaction := buildRegTx(change)
	exactFee := tx.RequiredFee(transaction, feeRate)
	if fee < exactFee {
		if change >= exactFee-fee {
			change -= exactFee - fee
			transaction = buildRegTx(change)
		} else {
			// Re-select with exact fee.
			selection, selErr = wallet.SelectCoins(nativeUTXOs, burnAmount+exactFee)
			if selErr != nil {
				return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("coin selection: %v (need burn + fee)", selErr)}
			}
			change = selection.Total - burnAmount - exactFee
			transaction = buildRegTx(change)
		}
	}

	// Add to mempool.
	_, poolErr := s.pool.Add(transaction)
	if poolErr != nil {
		return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("rejected: %v", poolErr)}
	}

	// Broadcast to P2P network.
	if s.p2pNode != nil {
		if err := s.p2pNode.BroadcastTx(transaction); err != nil {
			s.logger.Warn().Err(err).Msg("Failed to broadcast sub-chain registration tx")
		}
	}

	// Track change address and advance index.
	if change > 0 {
		changeKey, _ := master.DeriveAddress(0, wallet.ChangeInternal, changeIdx)
		changeAddr := changeKey.Address()
		_ = s.keystore.AddAccount(params.Name, wallet.AccountEntry{
			Index:   changeIdx,
			Change:  wallet.ChangeInternal,
			Name:    fmt.Sprintf("Change %d", changeIdx),
			Address: changeAddr.String(),
		})
		if err := s.keystore.IncrementChangeIndex(params.Name); err != nil {
			s.logger.Warn().Err(err).Msg("Failed to update change index")
		}
	}

	// Compute chain ID deterministically.
	chainID := subchain.DeriveChainID(transaction.Hash(), 0)

	return &WalletCreateSubChainResult{
		TxHash:  transaction.Hash().String(),
		ChainID: hex.EncodeToString(chainID[:]),
	}, nil
}

func (s *Server) handleSubChainSend(req *Request) (interface{}, *Error) {
	if err := s.requireWallet(); err != nil {
		return nil, err
	}
	if err := s.requireSubChainManager(); err != nil {
		return nil, err
	}

	var params SubChainSendParam
	if err := parseParams(req, &params); err != nil {
		return nil, err
	}
	if params.ChainID == "" || params.Name == "" || params.Password == "" || params.To == "" || params.Amount == 0 {
		return nil, &Error{Code: CodeInvalidParams, Message: "chain_id, name, password, to, and amount are required"}
	}

	// Resolve sub-chain.
	chainIDBytes, decErr := hex.DecodeString(params.ChainID)
	if decErr != nil || len(chainIDBytes) != types.HashSize {
		return nil, &Error{Code: CodeInvalidParams, Message: "invalid chain_id: must be 32-byte hex"}
	}
	var chainID types.ChainID
	copy(chainID[:], chainIDBytes)

	sr, ok := s.scManager.GetChain(chainID)
	if !ok {
		return nil, &Error{Code: CodeNotFound, Message: fmt.Sprintf("sub-chain %s not synced on this node", params.ChainID)}
	}

	// Parse recipient address.
	recipientAddr, addrErr := decodeAddress(params.To)
	if addrErr != nil {
		return nil, addrErr
	}

	// Load wallet.
	seed, loadErr := s.keystore.Load(params.Name, []byte(params.Password))
	if loadErr != nil {
		s.logger.Debug().Err(loadErr).Msg("wallet load failed")
		return nil, &Error{Code: CodeInvalidParams, Message: "invalid wallet name or password"}
	}

	master, masterErr := wallet.NewMasterKey(seed)
	for i := range seed {
		seed[i] = 0
	}
	if masterErr != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("derive master key: %v", masterErr)}
	}

	// Collect UTXOs from all wallet addresses on the sub-chain's UTXO store.
	wset, collectErr := s.collectWalletUTXOs(master, params.Name, sr.UTXOs, sr.Chain.Height())
	if collectErr != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("collect utxos: %v", collectErr)}
	}
	defer wset.zeroSigners()
	nativeUTXOs := filterNativeUTXOs(wset.utxos)
	if len(nativeUTXOs) == 0 {
		return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("no UTXOs found for wallet on sub-chain %s", params.ChainID)}
	}

	// Fee estimation with iterative coin selection.
	feeRate := sr.Genesis.Protocol.Consensus.MinFeeRate
	fee := tx.EstimateTxFee(1, 2, feeRate) // 1 input, 2 outputs (recipient + change)
	selection, selErr := wallet.SelectCoins(nativeUTXOs, params.Amount+fee)
	if selErr != nil {
		return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("coin selection: %v", selErr)}
	}
	// Recalculate fee with actual input count.
	fee = tx.EstimateTxFee(len(selection.Inputs), 2, feeRate)
	if selection.Total < params.Amount+fee {
		selection, selErr = wallet.SelectCoins(nativeUTXOs, params.Amount+fee)
		if selErr != nil {
			return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("coin selection: %v", selErr)}
		}
		fee = tx.EstimateTxFee(len(selection.Inputs), 2, feeRate)
	}
	change := selection.Total - params.Amount - fee

	// Build transaction.
	builder := tx.NewBuilder()
	for _, input := range selection.Inputs {
		builder.AddInput(input.Outpoint)
	}

	// Recipient output.
	recipientScript := types.Script{
		Type: types.ScriptTypeP2PKH,
		Data: recipientAddr.Bytes(),
	}
	builder.AddOutput(params.Amount, recipientScript)

	// Change output.
	var changeIdx uint32
	if change > 0 {
		var chErr error
		changeIdx, chErr = s.keystore.GetChangeIndex(params.Name)
		if chErr != nil {
			return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("get change index: %v", chErr)}
		}
		changeKey, chKeyErr := master.DeriveAddress(0, wallet.ChangeInternal, changeIdx)
		if chKeyErr != nil {
			return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("derive change address: %v", chKeyErr)}
		}
		changeScript := types.Script{
			Type: types.ScriptTypeP2PKH,
			Data: changeKey.Address().Bytes(),
		}
		builder.AddOutput(change, changeScript)
	}

	// Sign with per-input keys.
	if err := builder.SignMulti(wset.signers, wset.addrByOutpoint); err != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("sign transaction: %v", err)}
	}

	transaction := builder.Build()

	// Add to sub-chain mempool (NOT root).
	_, poolErr := sr.Pool.Add(transaction)
	if poolErr != nil {
		return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("rejected: %v", poolErr)}
	}

	// Broadcast to sub-chain P2P topic.
	if s.p2pNode != nil {
		idHex := hex.EncodeToString(chainID[:])
		if err := s.p2pNode.BroadcastSubChainTx(idHex, transaction); err != nil {
			s.logger.Warn().Err(err).Str("chain", params.ChainID).Msg("Failed to broadcast sub-chain tx")
		}
	}

	// Track change address and advance index.
	if change > 0 {
		changeKey, _ := master.DeriveAddress(0, wallet.ChangeInternal, changeIdx)
		changeAddr := changeKey.Address()
		_ = s.keystore.AddAccount(params.Name, wallet.AccountEntry{
			Index:   changeIdx,
			Change:  wallet.ChangeInternal,
			Name:    fmt.Sprintf("Change %d", changeIdx),
			Address: changeAddr.String(),
		})
		if err := s.keystore.IncrementChangeIndex(params.Name); err != nil {
			s.logger.Warn().Err(err).Msg("Failed to update change index")
		}
	}

	return &SubChainSendResult{
		TxHash: transaction.Hash().String(),
	}, nil
}

func (s *Server) handleSubChainStake(req *Request) (interface{}, *Error) {
	if err := s.requireWallet(); err != nil {
		return nil, err
	}
	if err := s.requireSubChainManager(); err != nil {
		return nil, err
	}

	var params SubChainStakeParam
	if err := parseParams(req, &params); err != nil {
		return nil, err
	}
	if params.ChainID == "" || params.Name == "" || params.Password == "" || params.Amount == 0 {
		return nil, &Error{Code: CodeInvalidParams, Message: "chain_id, name, password, and amount are required"}
	}

	// Resolve sub-chain.
	chainIDBytes, decErr := hex.DecodeString(params.ChainID)
	if decErr != nil || len(chainIDBytes) != types.HashSize {
		return nil, &Error{Code: CodeInvalidParams, Message: "invalid chain_id: must be 32-byte hex"}
	}
	var chainID types.ChainID
	copy(chainID[:], chainIDBytes)

	sr, ok := s.scManager.GetChain(chainID)
	if !ok {
		return nil, &Error{Code: CodeNotFound, Message: fmt.Sprintf("sub-chain %s not synced on this node", params.ChainID)}
	}

	// Validate amount == sub-chain validator stake (exact match required).
	requiredStake := sr.Genesis.Protocol.Consensus.ValidatorStake
	if requiredStake > 0 && params.Amount != requiredStake {
		return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("stake must be exactly %d, got %d", requiredStake, params.Amount)}
	}

	// Load wallet.
	seed, loadErr := s.keystore.Load(params.Name, []byte(params.Password))
	if loadErr != nil {
		s.logger.Debug().Err(loadErr).Msg("wallet load failed")
		return nil, &Error{Code: CodeInvalidParams, Message: "invalid wallet name or password"}
	}

	master, masterErr := wallet.NewMasterKey(seed)
	for i := range seed {
		seed[i] = 0
	}
	if masterErr != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("derive master key: %v", masterErr)}
	}

	// Derive account 0 for the stake pubkey.
	hdKey, derErr := master.DeriveAddress(0, wallet.ChangeExternal, 0)
	if derErr != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("derive address key: %v", derErr)}
	}
	pubKeyBytes := hdKey.PublicKeyBytes()

	// Block duplicate active/pending stake for this validator pubkey on this sub-chain.
	existingStakes, stakeErr := sr.UTXOs.GetStakes(pubKeyBytes)
	if stakeErr != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("get existing stakes: %v", stakeErr)}
	}
	if len(existingStakes) > 0 || hasPendingStakeForPubKey(sr.Pool.SelectForBlock(sr.Pool.Count()), pubKeyBytes) {
		return nil, &Error{Code: CodeInvalidParams, Message: "validator already has an active or pending stake on this sub-chain; unstake before staking again"}
	}

	// Collect UTXOs from all wallet addresses on the sub-chain's UTXO store.
	wset, collectErr := s.collectWalletUTXOs(master, params.Name, sr.UTXOs, sr.Chain.Height())
	if collectErr != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("collect utxos: %v", collectErr)}
	}
	defer wset.zeroSigners()
	nativeUTXOs := filterNativeUTXOs(wset.utxos)
	if len(nativeUTXOs) == 0 {
		return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("no UTXOs found for wallet on sub-chain %s", params.ChainID)}
	}

	// Fee estimation with iterative coin selection.
	feeRate := sr.Genesis.Protocol.Consensus.MinFeeRate
	fee := tx.EstimateTxFee(1, 2, feeRate) // 1 input, 2 outputs (stake + change)
	selection, selErr := wallet.SelectCoins(nativeUTXOs, params.Amount+fee)
	if selErr != nil {
		return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("coin selection: %v", selErr)}
	}
	// Recalculate fee with actual input count.
	fee = tx.EstimateTxFee(len(selection.Inputs), 2, feeRate)
	if selection.Total < params.Amount+fee {
		selection, selErr = wallet.SelectCoins(nativeUTXOs, params.Amount+fee)
		if selErr != nil {
			return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("coin selection: %v", selErr)}
		}
		fee = tx.EstimateTxFee(len(selection.Inputs), 2, feeRate)
	}
	change := selection.Total - params.Amount - fee

	// Stake output: ScriptTypeStake, data = 33-byte compressed pubkey.
	stakeScript := types.Script{
		Type: types.ScriptTypeStake,
		Data: pubKeyBytes,
	}

	// Get change index for the change output.
	var changeIdx uint32
	if change > 0 {
		var chErr error
		changeIdx, chErr = s.keystore.GetChangeIndex(params.Name)
		if chErr != nil {
			return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("get change index: %v", chErr)}
		}
	}

	// Build, check exact fee, and rebuild if needed.
	buildStakeTx := func(ch uint64) *tx.Transaction {
		b := tx.NewBuilder()
		for _, input := range selection.Inputs {
			b.AddInput(input.Outpoint)
		}
		b.AddOutput(params.Amount, stakeScript)
		if ch > 0 {
			chKey, _ := master.DeriveAddress(0, wallet.ChangeInternal, changeIdx)
			b.AddOutput(ch, types.Script{
				Type: types.ScriptTypeP2PKH,
				Data: chKey.Address().Bytes(),
			})
		}
		b.SignMulti(wset.signers, wset.addrByOutpoint)
		return b.Build()
	}

	transaction := buildStakeTx(change)
	exactFee := tx.RequiredFee(transaction, feeRate)
	if fee < exactFee {
		if change >= exactFee-fee {
			change -= exactFee - fee
			transaction = buildStakeTx(change)
		} else {
			// Re-select with exact fee.
			selection, selErr = wallet.SelectCoins(nativeUTXOs, params.Amount+exactFee)
			if selErr != nil {
				return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("coin selection: %v", selErr)}
			}
			change = selection.Total - params.Amount - exactFee
			transaction = buildStakeTx(change)
		}
	}

	// Add to sub-chain mempool.
	_, poolErr := sr.Pool.Add(transaction)
	if poolErr != nil {
		return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("rejected: %v", poolErr)}
	}

	// Broadcast to sub-chain P2P topic.
	if s.p2pNode != nil {
		idHex := hex.EncodeToString(chainID[:])
		if err := s.p2pNode.BroadcastSubChainTx(idHex, transaction); err != nil {
			s.logger.Warn().Err(err).Str("chain", params.ChainID).Msg("Failed to broadcast sub-chain stake tx")
		}
	}

	// Track change address and advance index.
	if change > 0 {
		changeKey, _ := master.DeriveAddress(0, wallet.ChangeInternal, changeIdx)
		changeAddr := changeKey.Address()
		_ = s.keystore.AddAccount(params.Name, wallet.AccountEntry{
			Index:   changeIdx,
			Change:  wallet.ChangeInternal,
			Name:    fmt.Sprintf("Change %d", changeIdx),
			Address: changeAddr.String(),
		})
		if err := s.keystore.IncrementChangeIndex(params.Name); err != nil {
			s.logger.Warn().Err(err).Msg("Failed to update change index")
		}
	}

	return &SubChainStakeResult{
		TxHash: transaction.Hash().String(),
		PubKey: hex.EncodeToString(pubKeyBytes),
	}, nil
}

func (s *Server) handleSubChainUnstake(req *Request) (interface{}, *Error) {
	if err := s.requireWallet(); err != nil {
		return nil, err
	}
	if err := s.requireSubChainManager(); err != nil {
		return nil, err
	}

	var params SubChainUnstakeParam
	if err := parseParams(req, &params); err != nil {
		return nil, err
	}
	if params.ChainID == "" || params.Name == "" || params.Password == "" {
		return nil, &Error{Code: CodeInvalidParams, Message: "chain_id, name, and password are required"}
	}

	// Resolve sub-chain.
	chainIDBytes, decErr := hex.DecodeString(params.ChainID)
	if decErr != nil || len(chainIDBytes) != types.HashSize {
		return nil, &Error{Code: CodeInvalidParams, Message: "invalid chain_id: must be 32-byte hex"}
	}
	var chainID types.ChainID
	copy(chainID[:], chainIDBytes)

	sr, ok := s.scManager.GetChain(chainID)
	if !ok {
		return nil, &Error{Code: CodeNotFound, Message: fmt.Sprintf("sub-chain %s not synced on this node", params.ChainID)}
	}

	// Load wallet.
	seed, loadErr := s.keystore.Load(params.Name, []byte(params.Password))
	if loadErr != nil {
		s.logger.Debug().Err(loadErr).Msg("wallet load failed")
		return nil, &Error{Code: CodeInvalidParams, Message: "invalid wallet name or password"}
	}

	master, masterErr := wallet.NewMasterKey(seed)
	for i := range seed {
		seed[i] = 0
	}
	if masterErr != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("derive master key: %v", masterErr)}
	}

	hdKey, derErr := master.DeriveAddress(0, wallet.ChangeExternal, 0)
	if derErr != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("derive address key: %v", derErr)}
	}

	signer, sigErr := hdKey.Signer()
	if sigErr != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("get signer: %v", sigErr)}
	}
	defer signer.Zero()
	senderAddr := hdKey.Address()
	pubKeyBytes := hdKey.PublicKeyBytes()

	// Fetch all stake UTXOs for this pubkey on the sub-chain.
	stakes, stakeErr := sr.UTXOs.GetStakes(pubKeyBytes)
	if stakeErr != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("get stakes: %v", stakeErr)}
	}
	if len(stakes) == 0 {
		return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("no active stakes found on sub-chain %s", params.ChainID)}
	}

	// Sum total staked.
	var totalStaked uint64
	for _, st := range stakes {
		totalStaked += st.Value
	}

	// Fee estimation from sub-chain's genesis.
	feeRate := sr.Genesis.Protocol.Consensus.MinFeeRate
	fee := tx.EstimateTxFee(len(stakes), 1, feeRate)
	if totalStaked <= fee {
		return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("staked amount %d too small to cover fee %d", totalStaked, fee)}
	}

	// Build transaction: inputs = all stake UTXOs, output = P2PKH to sender.
	builder := tx.NewBuilder()

	signers := map[types.Address]*crypto.PrivateKey{senderAddr: signer}
	outpointAddr := make(map[types.Outpoint]types.Address, len(stakes))
	for _, st := range stakes {
		builder.AddInput(st.Outpoint)
		outpointAddr[st.Outpoint] = senderAddr
	}

	// Return output: total staked minus fee.
	returnScript := types.Script{
		Type: types.ScriptTypeP2PKH,
		Data: senderAddr.Bytes(),
	}
	builder.AddOutput(totalStaked-fee, returnScript)

	// Sign with per-input keys.
	if err := builder.SignMulti(signers, outpointAddr); err != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("sign transaction: %v", err)}
	}

	transaction := builder.Build()

	// Add to sub-chain mempool.
	_, poolErr := sr.Pool.Add(transaction)
	if poolErr != nil {
		return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("rejected: %v", poolErr)}
	}

	// Broadcast to sub-chain P2P topic.
	if s.p2pNode != nil {
		idHex := hex.EncodeToString(chainID[:])
		if err := s.p2pNode.BroadcastSubChainTx(idHex, transaction); err != nil {
			s.logger.Warn().Err(err).Str("chain", params.ChainID).Msg("Failed to broadcast sub-chain unstake tx")
		}
	}

	return &SubChainUnstakeResult{
		TxHash: transaction.Hash().String(),
		Amount: totalStaked,
		PubKey: hex.EncodeToString(pubKeyBytes),
	}, nil
}

func (s *Server) handleWalletGetHistory(req *Request) (interface{}, *Error) {
	if err := s.requireWallet(); err != nil {
		return nil, err
	}

	var params WalletGetHistoryParam
	if err := parseParams(req, &params); err != nil {
		return nil, err
	}
	if params.Name == "" || params.Password == "" {
		return nil, &Error{Code: CodeInvalidParams, Message: "name and password are required"}
	}

	limit := params.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := params.Offset
	if offset < 0 {
		offset = 0
	}

	// Verify password by loading wallet.
	seed, loadErr := s.keystore.Load(params.Name, []byte(params.Password))
	if loadErr != nil {
		s.logger.Debug().Err(loadErr).Msg("wallet load failed")
		return nil, &Error{Code: CodeInvalidParams, Message: "invalid wallet name or password"}
	}
	for i := range seed {
		seed[i] = 0
	}

	// Gather all wallet addresses into a set.
	accounts, accErr := s.keystore.ListAccounts(params.Name)
	if accErr != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("list accounts: %v", accErr)}
	}

	addrSet := make(map[types.Address]bool, len(accounts))
	for _, a := range accounts {
		addr, err := types.ParseAddress(a.Address)
		if err == nil {
			addrSet[addr] = true
		}
	}

	if len(addrSet) == 0 {
		return &WalletGetHistoryResult{Total: 0, Entries: []TxHistoryEntry{}}, nil
	}

	// If we have a persistent index, use the indexed path.
	if s.txIndex != nil {
		return s.getHistoryIndexed(params.Name, "root", addrSet, limit, offset)
	}

	// Fallback: scan blocks from tip down (newest first).
	return s.getHistoryFallback(addrSet, limit, offset)
}

// getHistoryIndexed uses the persistent WalletTxIndex. It incrementally
// indexes new blocks since the last call, handles reorgs by rolling back
// entries above the current tip, then queries the index.
func (s *Server) getHistoryIndexed(walletName, chainID string, addrSet map[types.Address]bool, limit, offset int) (interface{}, *Error) {
	tipHeight := s.chain.Height()

	meta, err := s.txIndex.GetMeta(walletName, chainID)
	if err != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("read index: %v", err)}
	}

	// Reorg detection: if tip is below last indexed height, roll back.
	if meta.Count > 0 && tipHeight < meta.LastHeight {
		if err := s.txIndex.DeleteAbove(walletName, chainID, tipHeight); err != nil {
			return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("reorg rollback: %v", err)}
		}
		meta.LastHeight = tipHeight
	}

	// Incremental indexing: scan blocks from (lastHeight+1) to tipHeight.
	var startHeight uint64
	if meta.Count == 0 {
		startHeight = 0 // Fresh index, scan from genesis.
	} else {
		startHeight = meta.LastHeight + 1
	}

	if startHeight <= tipHeight {
		classifyFn := func(transaction interface{}, txIdx int, as map[types.Address]bool, blk interface{}) *TxHistoryEntry {
			txn, ok := transaction.(*tx.Transaction)
			if !ok {
				return nil
			}
			blkTyped, ok := blk.(interface{ Hash() types.Hash })
			if !ok {
				return nil
			}
			return s.classifyTx(txn, txIdx, as, blkTyped)
		}

		if _, err := s.txIndex.IndexBlocks(walletName, chainID, s.chain, startHeight, tipHeight, addrSet, classifyFn); err != nil {
			return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("index blocks: %v", err)}
		}
	}

	// Query the index.
	entries, total, err := s.txIndex.Query(walletName, chainID, limit, offset)
	if err != nil {
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("query index: %v", err)}
	}

	return &WalletGetHistoryResult{
		Total:   total,
		Entries: entries,
	}, nil
}

// getHistoryFallback scans blocks from tip down without an index.
// Capped at 1000 entries to bound response time.
func (s *Server) getHistoryFallback(addrSet map[types.Address]bool, limit, offset int) (interface{}, *Error) {
	const maxEntries = 1000
	tipHeight := s.chain.Height()
	var entries []TxHistoryEntry

	for h := int64(tipHeight); h >= 0; h-- {
		blk, err := s.chain.GetBlockByHeight(uint64(h))
		if err != nil {
			continue
		}

		blockHash := blk.Hash().String()
		blockTime := blk.Header.Timestamp

		for txIdx, transaction := range blk.Transactions {
			entry := s.classifyTx(transaction, txIdx, addrSet, blk)
			if entry == nil {
				continue
			}
			entry.BlockHash = blockHash
			entry.Height = uint64(h)
			entry.Timestamp = blockTime
			entry.Confirmed = true
			entries = append(entries, *entry)
		}

		if len(entries) >= maxEntries {
			break
		}
	}

	total := len(entries)

	// Apply pagination.
	if offset >= total {
		return &WalletGetHistoryResult{Total: total, Entries: []TxHistoryEntry{}}, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	paged := entries[offset:end]

	return &WalletGetHistoryResult{
		Total:   total,
		Entries: paged,
	}, nil
}

// classifyTx determines if a transaction is relevant to the wallet and classifies it.
func (s *Server) classifyTx(transaction *tx.Transaction, txIdx int, addrSet map[types.Address]bool, blk interface{ Hash() types.Hash }) *TxHistoryEntry {
	txHash := transaction.Hash().String()
	isCoinbase := txIdx == 0 && len(transaction.Inputs) > 0 && transaction.Inputs[0].PrevOut.IsZero()

	// Calculate our input and output sums (KGX only, excluding token-colored outputs).
	var ourInputSum, otherOutputSum, ourOutputSum uint64
	var hasOurInputs bool
	var firstTo, firstFrom string

	// Token tracking: aggregate token amounts sent to us vs others.
	tokenFlows := make(map[types.TokenID]*tokenFlow)

	// Check outputs.
	for _, out := range transaction.Outputs {
		addr := scriptToAddress(out.Script)
		isOurs := addr != nil && addrSet[*addr]

		if out.Token != nil {
			// Token-colored output.
			tf, ok := tokenFlows[out.Token.ID]
			if !ok {
				tf = &tokenFlow{}
				tokenFlows[out.Token.ID] = tf
			}
			if isOurs {
				tf.ourAmount += out.Token.Amount
			} else {
				tf.otherAmount += out.Token.Amount
				if tf.firstTo == "" && addr != nil {
					tf.firstTo = addr.String()
				}
			}
		} else {
			// Plain KGX output.
			if isOurs {
				ourOutputSum += out.Value
			} else {
				otherOutputSum += out.Value
				if firstTo == "" && addr != nil {
					firstTo = addr.String()
				}
			}
		}
	}

	// Check inputs (skip coinbase). Track input addresses for self-send detection.
	inputAddrs := make(map[types.Address]bool)
	if !isCoinbase {
		for _, in := range transaction.Inputs {
			if in.PrevOut.IsZero() {
				continue
			}
			// Derive address from the input's pubkey.
			if len(in.PubKey) == 33 {
				addr := crypto.AddressFromPubKey(in.PubKey)
				inputAddrs[addr] = true
				if addrSet[addr] {
					hasOurInputs = true
					// Look up the input value from the previous tx output.
					prevTx, err := s.chain.GetTransaction(in.PrevOut.TxID)
					if err == nil && int(in.PrevOut.Index) < len(prevTx.Outputs) {
						ourInputSum += prevTx.Outputs[in.PrevOut.Index].Value
					}
				} else if firstFrom == "" {
					firstFrom = addr.String()
				}
			}
		}
	}

	// Classify.
	var entry *TxHistoryEntry

	switch {
	case isCoinbase && ourOutputSum > 0:
		// Mined block reward.
		entry = &TxHistoryEntry{
			TxHash: txHash,
			Type:   "mined",
			Amount: formatAmount(ourOutputSum),
			Fee:    "0.000000000000",
		}

	case hasStakeOutput(transaction) && hasOurInputs:
		// Staking tx.
		var stakeAmt uint64
		for _, out := range transaction.Outputs {
			if out.Script.Type == types.ScriptTypeStake {
				stakeAmt += out.Value
			}
		}
		entry = &TxHistoryEntry{
			TxHash: txHash,
			Type:   "staked",
			Amount: formatAmount(stakeAmt),
			Fee:    formatAmount(safeSub(ourInputSum, totalOutputs(transaction))),
		}

	case hasStakeInput(transaction, s) && hasOurInputs:
		// Unstaking tx (spending stake UTXOs).
		entry = &TxHistoryEntry{
			TxHash: txHash,
			Type:   "unstaked",
			Amount: formatAmount(ourOutputSum),
			Fee:    formatAmount(safeSub(ourInputSum, totalOutputs(transaction))),
		}

	case hasMintOutput(transaction) && hasOurInputs:
		// Mint tx — also capture the minted token details.
		fee := safeSub(ourInputSum, totalOutputs(transaction))
		entry = &TxHistoryEntry{
			TxHash: txHash,
			Type:   "mint",
			Amount: formatAmount(fee),
			Fee:    formatAmount(fee),
		}
		// Find the minted token (first output with Token data).
		for _, out := range transaction.Outputs {
			if out.Token != nil {
				entry.TokenID = hex.EncodeToString(out.Token.ID[:])
				entry.TokenAmount = out.Token.Amount
				break
			}
		}

	case hasOurInputs && hasTokenOutputs(transaction):
		// Token transfer sent by us.
		fee := safeSub(ourInputSum, totalOutputs(transaction))

		// Find the primary token being sent (largest other-amount).
		var bestID types.TokenID
		var bestAmt uint64
		var bestTo string
		for tid, tf := range tokenFlows {
			if tf.otherAmount > bestAmt {
				bestID = tid
				bestAmt = tf.otherAmount
				bestTo = tf.firstTo
			}
		}

		// Self-send token: all token outputs go to us.
		if bestAmt == 0 {
			for tid, tf := range tokenFlows {
				if tf.ourAmount > 0 {
					bestID = tid
					bestAmt = tf.ourAmount
					break
				}
			}
		}

		entry = &TxHistoryEntry{
			TxHash:      txHash,
			Type:        "token_sent",
			Amount:      formatAmount(fee),
			Fee:         formatAmount(fee),
			To:          bestTo,
			TokenID:     hex.EncodeToString(bestID[:]),
			TokenAmount: bestAmt,
		}

	case hasOurInputs:
		// Sent by us (plain KGX).
		fee := safeSub(ourInputSum, totalOutputs(transaction))
		sentAmount := otherOutputSum
		sentTo := firstTo

		// Self-send: all outputs go to our addresses. Use the first output
		// going to a non-input address as the sent amount (tx builder adds
		// the send output before the change output).
		if otherOutputSum == 0 {
			for _, out := range transaction.Outputs {
				addr := scriptToAddress(out.Script)
				if addr != nil && !inputAddrs[*addr] {
					sentAmount = out.Value
					sentTo = addr.String()
					break
				}
			}
		}

		entry = &TxHistoryEntry{
			TxHash: txHash,
			Type:   "sent",
			Amount: formatAmount(sentAmount),
			Fee:    formatAmount(fee),
			To:     sentTo,
		}

	case hasTokenReceivedByUs(tokenFlows):
		// Token received (we got token outputs but didn't fund the tx).
		var bestID types.TokenID
		var bestAmt uint64
		for tid, tf := range tokenFlows {
			if tf.ourAmount > bestAmt {
				bestID = tid
				bestAmt = tf.ourAmount
			}
		}
		entry = &TxHistoryEntry{
			TxHash:      txHash,
			Type:        "token_received",
			Amount:      formatAmount(ourOutputSum),
			Fee:         "0.000000000000",
			From:        firstFrom,
			TokenID:     hex.EncodeToString(bestID[:]),
			TokenAmount: bestAmt,
		}

	case ourOutputSum > 0:
		// Received (plain KGX).
		entry = &TxHistoryEntry{
			TxHash: txHash,
			Type:   "received",
			Amount: formatAmount(ourOutputSum),
			Fee:    "0.000000000000",
			From:   firstFrom,
		}
	}

	return entry
}

// tokenFlow tracks token amounts per token ID in classifyTx.
type tokenFlow struct {
	ourAmount   uint64
	otherAmount uint64
	firstTo     string
}

// hasTokenReceivedByUs checks if any token flow has tokens going to our addresses.
func hasTokenReceivedByUs(flows map[types.TokenID]*tokenFlow) bool {
	for _, tf := range flows {
		if tf.ourAmount > 0 {
			return true
		}
	}
	return false
}

// scriptToAddress extracts an address from a P2PKH script.
func scriptToAddress(s types.Script) *types.Address {
	if s.Type == types.ScriptTypeP2PKH && len(s.Data) == types.AddressSize {
		var addr types.Address
		copy(addr[:], s.Data)
		return &addr
	}
	return nil
}

func hasStakeOutput(t *tx.Transaction) bool {
	for _, out := range t.Outputs {
		if out.Script.Type == types.ScriptTypeStake {
			return true
		}
	}
	return false
}

func hasStakeInput(t *tx.Transaction, s *Server) bool {
	for _, in := range t.Inputs {
		if in.PrevOut.IsZero() {
			continue
		}
		prevTx, err := s.chain.GetTransaction(in.PrevOut.TxID)
		if err != nil {
			continue
		}
		if int(in.PrevOut.Index) < len(prevTx.Outputs) {
			if prevTx.Outputs[in.PrevOut.Index].Script.Type == types.ScriptTypeStake {
				return true
			}
		}
	}
	return false
}

func hasMintOutput(t *tx.Transaction) bool {
	for _, out := range t.Outputs {
		if out.Script.Type == types.ScriptTypeMint {
			return true
		}
	}
	return false
}

func hasTokenOutputs(t *tx.Transaction) bool {
	for _, out := range t.Outputs {
		if out.Token != nil {
			return true
		}
	}
	return false
}

func totalOutputs(t *tx.Transaction) uint64 {
	var sum uint64
	for _, out := range t.Outputs {
		sum += out.Value
	}
	return sum
}

func safeSub(a, b uint64) uint64 {
	if a > b {
		return a - b
	}
	return 0
}

// handleWalletRescan re-derives wallet addresses and scans blocks from a given
// height to discover addresses that received funds. This is useful after
// importing a wallet or if the address index got out of sync.
func (s *Server) handleWalletRescan(req *Request) (interface{}, *Error) {
	if err := s.requireWallet(); err != nil {
		return nil, err
	}

	var params WalletRescanParam
	if err := parseParams(req, &params); err != nil {
		return nil, err
	}
	if params.Name == "" || params.Password == "" {
		return nil, &Error{Code: CodeInvalidParams, Message: "name and password are required"}
	}

	// Load wallet seed.
	seed, loadErr := s.keystore.Load(params.Name, []byte(params.Password))
	if loadErr != nil {
		return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("open wallet: %v", loadErr)}
	}
	master, masterErr := wallet.NewMasterKey(seed)
	if masterErr != nil {
		for i := range seed {
			seed[i] = 0
		}
		return nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("derive master key: %v", masterErr)}
	}
	for i := range seed {
		seed[i] = 0
	}

	// Collect existing known addresses so we can count new discoveries.
	existingAccounts, _ := s.keystore.ListAccounts(params.Name)
	existing := make(map[string]bool, len(existingAccounts))
	for _, a := range existingAccounts {
		existing[a.Address] = true
	}

	// Phase 1: Derive addresses and build a lookup set.
	// Default supports exchange-style wallets with many deposit addresses.
	deriveLimit := uint32(2000)
	if extIdx, err := s.keystore.GetExternalIndex(params.Name); err == nil && extIdx+20 > deriveLimit {
		deriveLimit = extIdx + 20
	}
	if chgIdx, err := s.keystore.GetChangeIndex(params.Name); err == nil && chgIdx+20 > deriveLimit {
		deriveLimit = chgIdx + 20
	}
	if params.DeriveLimit > 0 {
		deriveLimit = params.DeriveLimit
	}
	const maxDeriveLimit = uint32(100000)
	if deriveLimit > maxDeriveLimit {
		return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("derive_limit too high: max %d", maxDeriveLimit)}
	}
	type derivedAddr struct {
		address types.Address
		change  uint32
		index   uint32
	}
	var derived []derivedAddr
	addrSet := make(map[types.Address]bool)

	for _, ch := range []uint32{wallet.ChangeExternal, wallet.ChangeInternal} {
		for idx := uint32(0); idx < deriveLimit; idx++ {
			hdKey, err := master.DeriveAddress(0, ch, idx)
			if err != nil {
				break
			}
			addr := hdKey.Address()
			derived = append(derived, derivedAddr{address: addr, change: ch, index: idx})
			addrSet[addr] = true
		}
	}

	// Resolve chain and UTXO store (root or sub-chain).
	scanChain := s.chain
	var scanUTXOs utxoGetter = s.utxos
	if params.ChainID != "" {
		if err := s.requireSubChainManager(); err != nil {
			return nil, err
		}
		chainIDBytes, decErr := hex.DecodeString(params.ChainID)
		if decErr != nil || len(chainIDBytes) != types.HashSize {
			return nil, &Error{Code: CodeInvalidParams, Message: "invalid chain_id: must be 32-byte hex"}
		}
		var chainID types.ChainID
		copy(chainID[:], chainIDBytes)
		sr, ok := s.scManager.GetChain(chainID)
		if !ok {
			return nil, &Error{Code: CodeNotFound, Message: fmt.Sprintf("sub-chain %s not synced on this node", params.ChainID)}
		}
		scanChain = sr.Chain
		scanUTXOs = sr.UTXOs
	}

	// Phase 2: Scan blocks from fromHeight to tip, marking addresses that appear in outputs.
	tipHeight := scanChain.State().Height
	fromHeight := params.FromHeight
	if fromHeight > tipHeight {
		fromHeight = tipHeight
	}
	usedAddrs := make(map[types.Address]bool)

	for h := fromHeight; h <= tipHeight; h++ {
		blk, err := scanChain.GetBlockByHeight(h)
		if err != nil {
			continue
		}
		for _, txn := range blk.Transactions {
			for _, out := range txn.Outputs {
				var addr types.Address
				switch {
				case (out.Script.Type == types.ScriptTypeP2PKH || out.Script.Type == types.ScriptTypeMint) &&
					len(out.Script.Data) >= types.AddressSize:
					copy(addr[:], out.Script.Data[:types.AddressSize])
				case out.Script.Type == types.ScriptTypeStake && len(out.Script.Data) == 33:
					addr = crypto.AddressFromPubKey(out.Script.Data)
				default:
					continue
				}
				if addrSet[addr] {
					usedAddrs[addr] = true
				}
			}
		}
	}

	// Phase 3: Also check current UTXO set for any derived address (catches
	// addresses that received funds before fromHeight and still have UTXOs).
	// Checks both regular UTXOs (a/ prefix) and stake UTXOs (k/ prefix).
	for _, d := range derived {
		if usedAddrs[d.address] {
			continue
		}
		utxos, err := scanUTXOs.GetByAddress(d.address)
		if err == nil && len(utxos) > 0 {
			usedAddrs[d.address] = true
			continue
		}
		// Check stake UTXOs (indexed by pubkey, resolved via address match).
		if store, ok := scanUTXOs.(*utxo.Store); ok {
			stakes, sErr := stakesByAddress(store, d.address)
			if sErr == nil && len(stakes) > 0 {
				usedAddrs[d.address] = true
			}
		}
	}

	// Phase 4: Register all discovered addresses and track gap-limit indexes.
	addressesFound := len(usedAddrs)
	addressesNew := 0
	highestExternal := -1
	highestChange := -1

	for _, d := range derived {
		if !usedAddrs[d.address] {
			continue
		}
		addrStr := d.address.String()
		if !existing[addrStr] {
			addressesNew++
			namePrefix := "Address"
			if d.change == wallet.ChangeInternal {
				namePrefix = "Change"
			}
			_ = s.keystore.AddAccount(params.Name, wallet.AccountEntry{
				Index:   d.index,
				Change:  d.change,
				Name:    fmt.Sprintf("%s %d", namePrefix, d.index),
				Address: addrStr,
			})
		}
		if d.change == wallet.ChangeExternal && int(d.index) > highestExternal {
			highestExternal = int(d.index)
		}
		if d.change == wallet.ChangeInternal && int(d.index) > highestChange {
			highestChange = int(d.index)
		}
	}

	// Update derivation indexes.
	if highestExternal >= 0 {
		_ = s.keystore.SetExternalIndex(params.Name, uint32(highestExternal+1))
	}
	if highestChange >= 0 {
		_ = s.keystore.SetChangeIndex(params.Name, uint32(highestChange+1))
	}

	return &WalletRescanResult{
		AddressesFound: addressesFound,
		AddressesNew:   addressesNew,
		FromHeight:     fromHeight,
		ToHeight:       tipHeight,
	}, nil
}
