package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Klingon-tech/klingnet-chain/internal/rpc"
	"github.com/Klingon-tech/klingnet-chain/internal/rpcclient"
	"github.com/Klingon-tech/klingnet-chain/internal/wallet"
)

// WalletService exposes wallet operations to the frontend.
// All operations proxy through the klingnetd wallet RPC endpoints so that
// the QT app does not need local access to the keystore directory.
type WalletService struct {
	app *App
}

// WalletInfo is returned after wallet creation/import.
type WalletInfo struct {
	Name    string `json:"name"`
	Address string `json:"address"`
}

// AccountInfo describes a wallet account.
type AccountInfo struct {
	Index   uint32 `json:"index"`
	Change  uint32 `json:"change"` // 0=external, 1=internal/change
	Name    string `json:"name"`
	Address string `json:"address"`
}

// UTXOInfo describes an unspent output.
type UTXOInfo struct {
	TxID   string `json:"tx_id"`
	Index  uint32 `json:"index"`
	Value  string `json:"value"`
	Script uint8  `json:"script_type"`
}

// SendRequest holds the parameters for sending a transaction.
type SendRequest struct {
	WalletName string `json:"wallet_name"`
	Password   string `json:"password"`
	ToAddress  string `json:"to_address"`
	Amount     string `json:"amount"`
}

// SendResult is returned after a transaction is submitted.
type SendResult struct {
	TxHash string `json:"tx_hash"`
}

// ConsolidateResult is returned after a consolidation transaction is submitted.
type ConsolidateResult struct {
	TxHash       string `json:"tx_hash"`
	InputsUsed   uint32 `json:"inputs_used"`
	InputTotal   string `json:"input_total"`
	OutputAmount string `json:"output_amount"`
	Fee          string `json:"fee"`
}

// SendManyRecipient is a single recipient in a sendmany transaction.
type SendManyRecipient struct {
	ToAddress string `json:"to_address"`
	Amount    string `json:"amount"`
}

// SendManyRequest holds the parameters for a multi-recipient transaction.
type SendManyRequest struct {
	WalletName string              `json:"wallet_name"`
	Password   string              `json:"password"`
	Recipients []SendManyRecipient `json:"recipients"`
}

// SendManyResult is returned after a sendmany transaction is submitted.
type SendManyResult struct {
	TxHash string `json:"tx_hash"`
}

// StakeRequest holds the parameters for creating a stake transaction.
type StakeRequest struct {
	WalletName string `json:"wallet_name"`
	Password   string `json:"password"`
	Amount     string `json:"amount"`
}

// StakeResult is returned after a stake transaction is submitted.
type StakeResult struct {
	TxHash string `json:"tx_hash"`
	PubKey string `json:"pubkey"`
}

// UnstakeRequest holds the parameters for withdrawing a stake.
type UnstakeRequest struct {
	WalletName string `json:"wallet_name"`
	Password   string `json:"password"`
}

// UnstakeResult is returned after an unstake transaction is submitted.
type UnstakeResult struct {
	TxHash string `json:"tx_hash"`
	Amount string `json:"amount"`
	PubKey string `json:"pubkey"`
}

// MintTokenRequest holds the parameters for minting a new token.
type MintTokenRequest struct {
	WalletName string `json:"wallet_name"`
	Password   string `json:"password"`
	TokenName  string `json:"token_name"`
	Symbol     string `json:"symbol"`
	Decimals   uint8  `json:"decimals"`
	Amount     string `json:"amount"`
	Recipient  string `json:"recipient"`
}

// MintTokenResult is returned after a mint transaction is submitted.
type MintTokenResult struct {
	TxHash  string `json:"tx_hash"`
	TokenID string `json:"token_id"`
}

// ExportKeyResult is returned by ExportValidatorKey.
type ExportKeyResult struct {
	PrivateKey string `json:"private_key"`
	PubKey     string `json:"pubkey"`
	Address    string `json:"address"`
}

// NewAddressResult is returned by NewAddress.
type NewAddressResult struct {
	Index   uint32 `json:"index"`
	Address string `json:"address"`
}

// RescanResult is returned by RescanWallet.
type RescanResult struct {
	AddressesFound int    `json:"addresses_found"`
	AddressesNew   int    `json:"addresses_new"`
	FromHeight     uint64 `json:"from_height"`
	ToHeight       uint64 `json:"to_height"`
}

// ── Local-only helpers (no keys, no keystore) ────────────────────────

// GenerateMnemonic creates a new 24-word BIP-39 mnemonic.
func (w *WalletService) GenerateMnemonic() (string, error) {
	return wallet.GenerateMnemonic()
}

// ValidateMnemonic checks if a mnemonic phrase is valid.
func (w *WalletService) ValidateMnemonic(mnemonic string) bool {
	return wallet.ValidateMnemonic(mnemonic)
}

// ── Wallet management (via RPC) ──────────────────────────────────────

// CreateWallet creates a new wallet on the node via wallet_import RPC.
// The mnemonic is generated locally and passed to the node for storage.
func (w *WalletService) CreateWallet(name, password, mnemonic string) (*WalletInfo, error) {
	mnemonic = strings.Join(strings.Fields(mnemonic), " ")
	var result rpc.WalletImportResult
	if err := w.app.rpcClient().Call("wallet_import", rpc.WalletImportParam{
		Name:     name,
		Password: password,
		Mnemonic: mnemonic,
	}, &result); err != nil {
		return nil, err
	}
	return &WalletInfo{Name: name, Address: result.Address}, nil
}

// ImportWallet imports a wallet from an existing mnemonic via RPC.
func (w *WalletService) ImportWallet(name, password, mnemonic string) (*WalletInfo, error) {
	mnemonic = strings.Join(strings.Fields(mnemonic), " ")
	var result rpc.WalletImportResult
	if err := w.app.rpcClient().Call("wallet_import", rpc.WalletImportParam{
		Name:     name,
		Password: password,
		Mnemonic: mnemonic,
	}, &result); err != nil {
		return nil, err
	}
	return &WalletInfo{Name: name, Address: result.Address}, nil
}

// ListWallets returns the names of all wallets.
// Tries RPC first; falls back to local keystore (just reads filenames, no keys).
func (w *WalletService) ListWallets() ([]string, error) {
	var result rpc.WalletListResult
	if err := w.app.rpcClient().Call("wallet_list", nil, &result); err == nil {
		return result.Wallets, nil
	}
	// Fallback: read local keystore directory.
	ks, err := wallet.NewKeystore(w.app.keystorePath())
	if err != nil {
		return nil, err
	}
	return ks.List()
}

// GetWalletAccounts returns the accounts for a wallet via RPC.
func (w *WalletService) GetWalletAccounts(name, password string) ([]AccountInfo, error) {
	var result rpc.WalletAddressListResult
	if err := w.app.rpcClient().Call("wallet_listAddresses", rpc.WalletUnlockParam{
		Name:     name,
		Password: password,
	}, &result); err != nil {
		return nil, err
	}
	accounts := make([]AccountInfo, len(result.Accounts))
	for i, a := range result.Accounts {
		accounts[i] = AccountInfo{
			Index:   a.Index,
			Change:  a.Change,
			Name:    a.Name,
			Address: a.Address,
		}
	}
	// Cache addresses so balance works without unlock on next launch.
	w.app.SetKnownAccounts(name, accounts)
	return accounts, nil
}

// DeleteWallet removes a wallet file from the local keystore.
// This only works when the QT data directory matches the node's keystore path.
func (w *WalletService) DeleteWallet(name string) error {
	ks, err := wallet.NewKeystore(w.app.keystorePath())
	if err != nil {
		return fmt.Errorf("open keystore: %w", err)
	}
	return ks.Delete(name)
}

// NewAddress generates a new unused receiving address for the wallet.
func (w *WalletService) NewAddress(name, password string) (*NewAddressResult, error) {
	var result rpc.WalletAddressResult
	if err := w.app.rpcClient().Call("wallet_newAddress", rpc.WalletNewAddressParam{
		Name:     name,
		Password: password,
	}, &result); err != nil {
		return nil, err
	}
	return &NewAddressResult{
		Index:   result.Index,
		Address: result.Address,
	}, nil
}

// ── Balance & UTXO queries (already RPC) ─────────────────────────────

// BalanceBreakdown holds the balance breakdown for the frontend.
type BalanceBreakdown struct {
	Total     string `json:"total"`
	Spendable string `json:"spendable"`
	Immature  string `json:"immature"`
	Staked    string `json:"staked"`
	Locked    string `json:"locked"`
}

// GetBalance returns the balance breakdown for an address via RPC.
func (w *WalletService) GetBalance(address string) (*BalanceBreakdown, error) {
	var result rpc.BalanceResult
	if err := w.app.rpcClient().Call("utxo_getBalance", rpc.AddressParam{Address: address}, &result); err != nil {
		return nil, err
	}
	return &BalanceBreakdown{
		Total:     formatAmount(result.Balance),
		Spendable: formatAmount(result.Spendable),
		Immature:  formatAmount(result.Immature),
		Staked:    formatAmount(result.Staked),
		Locked:    formatAmount(result.Locked),
	}, nil
}

// GetTotalBalance sums the balance breakdown across multiple addresses.
func (w *WalletService) GetTotalBalance(addresses []string) (*BalanceBreakdown, error) {
	client := w.app.rpcClient()
	var total, spendable, immature, staked, locked uint64
	for _, addr := range addresses {
		var result rpc.BalanceResult
		if err := client.Call("utxo_getBalance", rpc.AddressParam{Address: addr}, &result); err != nil {
			continue
		}
		total += result.Balance
		spendable += result.Spendable
		immature += result.Immature
		staked += result.Staked
		locked += result.Locked
	}
	return &BalanceBreakdown{
		Total:     formatAmount(total),
		Spendable: formatAmount(spendable),
		Immature:  formatAmount(immature),
		Staked:    formatAmount(staked),
		Locked:    formatAmount(locked),
	}, nil
}

// GetUTXOs returns the UTXO list for an address via RPC.
func (w *WalletService) GetUTXOs(address string) ([]UTXOInfo, error) {
	var result rpc.UTXOListResult
	if err := w.app.rpcClient().Call("utxo_getByAddress", rpc.AddressParam{Address: address}, &result); err != nil {
		return nil, err
	}

	utxos := make([]UTXOInfo, len(result.UTXOs))
	for i, u := range result.UTXOs {
		utxos[i] = UTXOInfo{
			TxID:   u.Outpoint.TxID.String(),
			Index:  u.Outpoint.Index,
			Value:  formatAmount(u.Value),
			Script: uint8(u.Script.Type),
		}
	}
	return utxos, nil
}

// ── Transaction operations (via RPC) ─────────────────────────────────

// SendTransaction submits a transaction via wallet_send RPC.
func (w *WalletService) SendTransaction(req SendRequest) (*SendResult, error) {
	amount, err := parseAmount(req.Amount)
	if err != nil {
		return nil, fmt.Errorf("invalid amount: %w", err)
	}

	var result rpc.WalletSendResult
	if err := w.app.rpcClient().Call("wallet_send", rpc.WalletSendParam{
		Name:     req.WalletName,
		Password: req.Password,
		To:       req.ToAddress,
		Amount:   amount,
	}, &result); err != nil {
		return nil, err
	}
	return &SendResult{TxHash: result.TxHash}, nil
}

// ConsolidateUTXOs submits a consolidation transaction on the root chain.
func (w *WalletService) ConsolidateUTXOs(walletName, password string, maxInputs uint32) (*ConsolidateResult, error) {
	var result rpc.WalletConsolidateResult
	if err := w.app.rpcClient().Call("wallet_consolidate", rpc.WalletConsolidateParam{
		Name:      walletName,
		Password:  password,
		MaxInputs: maxInputs,
	}, &result); err != nil {
		return nil, err
	}
	return &ConsolidateResult{
		TxHash:       result.TxHash,
		InputsUsed:   result.InputsUsed,
		InputTotal:   formatAmount(result.InputTotal),
		OutputAmount: formatAmount(result.OutputAmount),
		Fee:          formatAmount(result.Fee),
	}, nil
}

// SendManyTransaction submits a multi-recipient transaction via wallet_sendMany RPC.
func (w *WalletService) SendManyTransaction(req SendManyRequest) (*SendManyResult, error) {
	recipients := make([]rpc.Recipient, len(req.Recipients))
	for i, r := range req.Recipients {
		amount, err := parseAmount(r.Amount)
		if err != nil {
			return nil, fmt.Errorf("recipient %d: invalid amount: %w", i, err)
		}
		recipients[i] = rpc.Recipient{To: r.ToAddress, Amount: amount}
	}

	var result rpc.WalletSendManyResult
	if err := w.app.rpcClient().Call("wallet_sendMany", rpc.WalletSendManyParam{
		Name:       req.WalletName,
		Password:   req.Password,
		Recipients: recipients,
	}, &result); err != nil {
		return nil, err
	}
	return &SendManyResult{TxHash: result.TxHash}, nil
}

// EstimateFee returns the estimated fee for a typical 1-in/2-out transaction.
func (w *WalletService) EstimateFee() (string, error) {
	var info rpc.MempoolInfoResult
	if err := w.app.rpcClient().Call("mempool_getInfo", nil, &info); err != nil {
		return "", err
	}
	rate := info.MinFeeRate
	if rate == 0 {
		rate = 1 // Fallback: 1 base unit per byte.
	}
	// Estimate for a typical 1-input, 2-output transaction.
	// SigningBytes: overhead(20) + 1*perInput(36) + 2*perOutput(33) = 122 bytes.
	fee := rate * 122
	return formatAmount(fee), nil
}

// StakeTransaction submits a staking transaction via wallet_stake RPC.
func (w *WalletService) StakeTransaction(req StakeRequest) (*StakeResult, error) {
	amount, err := parseAmount(req.Amount)
	if err != nil {
		return nil, fmt.Errorf("invalid amount: %w", err)
	}

	var result rpc.WalletStakeResult
	if err := w.app.rpcClient().Call("wallet_stake", rpc.WalletStakeParam{
		Name:     req.WalletName,
		Password: req.Password,
		Amount:   amount,
	}, &result); err != nil {
		return nil, err
	}
	return &StakeResult{
		TxHash: result.TxHash,
		PubKey: result.PubKey,
	}, nil
}

// UnstakeTransaction withdraws all validator stakes via wallet_unstake RPC.
func (w *WalletService) UnstakeTransaction(req UnstakeRequest) (*UnstakeResult, error) {
	var result rpc.WalletUnstakeResult
	if err := w.app.rpcClient().Call("wallet_unstake", rpc.WalletUnstakeParam{
		Name:     req.WalletName,
		Password: req.Password,
	}, &result); err != nil {
		return nil, err
	}
	return &UnstakeResult{
		TxHash: result.TxHash,
		Amount: formatAmount(result.Amount),
		PubKey: result.PubKey,
	}, nil
}

// MintToken submits a token minting transaction via wallet_mintToken RPC.
func (w *WalletService) MintToken(req MintTokenRequest) (*MintTokenResult, error) {
	amount, err := strconv.ParseUint(req.Amount, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid amount: %w", err)
	}
	if amount == 0 {
		return nil, fmt.Errorf("amount must be positive")
	}

	var result rpc.WalletMintTokenResult
	if err := w.app.rpcClient().Call("wallet_mintToken", rpc.WalletMintTokenParam{
		Name:      req.WalletName,
		Password:  req.Password,
		TokenName: req.TokenName,
		Symbol:    req.Symbol,
		Decimals:  req.Decimals,
		Amount:    amount,
		Recipient: req.Recipient,
	}, &result); err != nil {
		return nil, err
	}
	return &MintTokenResult{
		TxHash:  result.TxHash,
		TokenID: result.TokenID,
	}, nil
}

// SendTokenRequest holds the parameters for sending tokens.
type SendTokenRequest struct {
	WalletName string `json:"wallet_name"`
	Password   string `json:"password"`
	TokenID    string `json:"token_id"`
	ToAddress  string `json:"to_address"`
	Amount     string `json:"amount"` // Raw token units as string.
}

// SendTokenResult is returned after a token transfer is submitted.
type SendTokenResult struct {
	TxHash string `json:"tx_hash"`
}

// TokenBalanceInfo describes a single token balance.
type TokenBalanceInfo struct {
	TokenID  string `json:"token_id"`
	Amount   uint64 `json:"amount"`
	Name     string `json:"name"`
	Symbol   string `json:"symbol"`
	Decimals uint8  `json:"decimals"`
}

// TokenInfo describes a token in the registry.
type TokenInfo struct {
	TokenID  string `json:"token_id"`
	Name     string `json:"name"`
	Symbol   string `json:"symbol"`
	Decimals uint8  `json:"decimals"`
	Creator  string `json:"creator"`
}

// ── Token operations (via RPC) ────────────────────────────────────────

// GetTokenBalances returns aggregated token balances across multiple addresses.
func (w *WalletService) GetTokenBalances(addresses []string) ([]TokenBalanceInfo, error) {
	client := w.app.rpcClient()
	merged := make(map[string]*TokenBalanceInfo)

	for _, addr := range addresses {
		var result rpc.TokenBalanceResult
		if err := client.Call("token_getBalance", rpc.AddressParam{Address: addr}, &result); err != nil {
			continue
		}
		for _, t := range result.Tokens {
			if existing, ok := merged[t.TokenID]; ok {
				existing.Amount += t.Amount
			} else {
				merged[t.TokenID] = &TokenBalanceInfo{
					TokenID:  t.TokenID,
					Amount:   t.Amount,
					Name:     t.Name,
					Symbol:   t.Symbol,
					Decimals: t.Decimals,
				}
			}
		}
	}

	balances := make([]TokenBalanceInfo, 0, len(merged))
	for _, b := range merged {
		balances = append(balances, *b)
	}
	return balances, nil
}

// GetTokenList returns all known tokens from the node.
func (w *WalletService) GetTokenList() ([]TokenInfo, error) {
	var result rpc.TokenListResult
	if err := w.app.rpcClient().Call("token_list", nil, &result); err != nil {
		return nil, err
	}

	tokens := make([]TokenInfo, len(result.Tokens))
	for i, t := range result.Tokens {
		tokens[i] = TokenInfo{
			TokenID:  t.TokenID,
			Name:     t.Name,
			Symbol:   t.Symbol,
			Decimals: t.Decimals,
			Creator:  t.Creator,
		}
	}
	return tokens, nil
}

// SendToken submits a token transfer transaction via wallet_sendToken RPC.
func (w *WalletService) SendToken(req SendTokenRequest) (*SendTokenResult, error) {
	amount, err := strconv.ParseUint(req.Amount, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid amount: %w", err)
	}

	var result rpc.WalletSendTokenResult
	if err := w.app.rpcClient().Call("wallet_sendToken", rpc.WalletSendTokenParam{
		Name:     req.WalletName,
		Password: req.Password,
		TokenID:  req.TokenID,
		To:       req.ToAddress,
		Amount:   amount,
	}, &result); err != nil {
		return nil, err
	}
	return &SendTokenResult{TxHash: result.TxHash}, nil
}

// TxHistoryEntry describes a single transaction in wallet history.
type TxHistoryEntry struct {
	TxHash    string `json:"tx_hash"`
	BlockHash string `json:"block_hash"`
	Height    uint64 `json:"height"`
	Timestamp uint64 `json:"timestamp"`
	Type      string `json:"type"`
	Amount    string `json:"amount"`
	Fee       string `json:"fee"`
	To        string `json:"to,omitempty"`
	From      string `json:"from,omitempty"`
	Confirmed bool   `json:"confirmed"`
}

// TxHistoryResult is returned by GetTransactionHistory.
type TxHistoryResult struct {
	Total   int              `json:"total"`
	Entries []TxHistoryEntry `json:"entries"`
}

// GetTransactionHistory returns the transaction history for a wallet.
func (w *WalletService) GetTransactionHistory(name, password string, limit, offset int) (*TxHistoryResult, error) {
	var result rpc.WalletGetHistoryResult
	if err := w.app.rpcClient().Call("wallet_getHistory", rpc.WalletGetHistoryParam{
		Name:     name,
		Password: password,
		Limit:    limit,
		Offset:   offset,
	}, &result); err != nil {
		return nil, err
	}
	entries := make([]TxHistoryEntry, len(result.Entries))
	for i, e := range result.Entries {
		entries[i] = TxHistoryEntry{
			TxHash:    e.TxHash,
			BlockHash: e.BlockHash,
			Height:    e.Height,
			Timestamp: e.Timestamp,
			Type:      e.Type,
			Amount:    e.Amount,
			Fee:       e.Fee,
			To:        e.To,
			From:      e.From,
			Confirmed: e.Confirmed,
		}
	}
	return &TxHistoryResult{
		Total:   result.Total,
		Entries: entries,
	}, nil
}

// RescanWallet re-derives wallet addresses and scans blocks from a given height
// to discover addresses that received funds. Optional chainID scans a sub-chain.
func (w *WalletService) RescanWallet(name, password string, fromHeight uint64, chainID string) (*RescanResult, error) {
	var result rpc.WalletRescanResult
	// Deep rescans can take significantly longer than regular RPC calls.
	client := rpcclient.NewWithTimeout(w.app.rpcEndpoint, 10*time.Minute)
	if err := client.Call("wallet_rescan", rpc.WalletRescanParam{
		Name:       name,
		Password:   password,
		FromHeight: fromHeight,
		ChainID:    chainID,
	}, &result); err != nil {
		return nil, err
	}
	return &RescanResult{
		AddressesFound: result.AddressesFound,
		AddressesNew:   result.AddressesNew,
		FromHeight:     result.FromHeight,
		ToHeight:       result.ToHeight,
	}, nil
}

// ExportValidatorKey exports a validator private key via wallet_exportKey RPC.
func (w *WalletService) ExportValidatorKey(name, password string, account, index uint32) (*ExportKeyResult, error) {
	var result rpc.WalletExportKeyResult
	if err := w.app.rpcClient().Call("wallet_exportKey", rpc.WalletExportKeyParam{
		Name:     name,
		Password: password,
		Account:  account,
		Index:    index,
	}, &result); err != nil {
		return nil, err
	}
	return &ExportKeyResult{
		PrivateKey: result.PrivateKey,
		PubKey:     result.PubKey,
		Address:    result.Address,
	}, nil
}
