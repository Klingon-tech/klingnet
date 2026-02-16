package rpc

import (
	"github.com/Klingon-tech/klingnet-chain/internal/utxo"
	"github.com/Klingon-tech/klingnet-chain/pkg/block"
	"github.com/Klingon-tech/klingnet-chain/pkg/tx"
)

// JSON-RPC 2.0 error codes.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
	CodeNotFound       = -32000
)

// Request is a JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
	ID      interface{} `json:"id"`
}

// Response is a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string      `json:"jsonrpc"`
	Result  interface{} `json:"result,omitempty"`
	Error   *Error      `json:"error,omitempty"`
	ID      interface{} `json:"id"`
}

// Error is a JSON-RPC 2.0 error object.
type Error struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// ── Param types ─────────────────────────────────────────────────────────

// HashParam is used by endpoints that take a single hash.
type HashParam struct {
	Hash    string `json:"hash"`
	ChainID string `json:"chain_id,omitempty"`
}

// HeightParam is used by endpoints that take a block height.
type HeightParam struct {
	Height  uint64 `json:"height"`
	ChainID string `json:"chain_id,omitempty"`
}

// OutpointParam is used by utxo_get.
type OutpointParam struct {
	TxID    string `json:"tx_id"`
	Index   uint32 `json:"index"`
	ChainID string `json:"chain_id,omitempty"`
}

// AddressParam is used by utxo_getByAddress and utxo_getBalance.
type AddressParam struct {
	Address string `json:"address"`
	ChainID string `json:"chain_id,omitempty"`
}

// TxSubmitParam is used by tx_submit and tx_validate.
type TxSubmitParam struct {
	Transaction *tx.Transaction `json:"transaction"`
	ChainID     string          `json:"chain_id,omitempty"`
}

// ── Block/Tx result types ───────────────────────────────────────────────

// BlockResult wraps a block with its precomputed hash for RPC responses.
type BlockResult struct {
	Hash         string        `json:"hash"`
	Header       *block.Header `json:"header"`
	Transactions []*TxResult   `json:"transactions"`
}

// TxResult wraps a transaction with its precomputed hash for RPC responses.
type TxResult struct {
	Hash     string      `json:"hash"`
	Version  uint32      `json:"version"`
	Inputs   []tx.Input  `json:"inputs"`
	Outputs  []tx.Output `json:"outputs"`
	LockTime uint64      `json:"locktime"`
}

// NewBlockResult creates a BlockResult from a block, precomputing all hashes.
func NewBlockResult(b *block.Block) *BlockResult {
	txResults := make([]*TxResult, len(b.Transactions))
	for i, t := range b.Transactions {
		txResults[i] = NewTxResult(t)
	}
	return &BlockResult{
		Hash:         b.Hash().String(),
		Header:       b.Header,
		Transactions: txResults,
	}
}

// NewTxResult creates a TxResult from a transaction, precomputing its hash.
func NewTxResult(t *tx.Transaction) *TxResult {
	return &TxResult{
		Hash:     t.Hash().String(),
		Version:  t.Version,
		Inputs:   t.Inputs,
		Outputs:  t.Outputs,
		LockTime: t.LockTime,
	}
}

// ── Result types ────────────────────────────────────────────────────────

// ChainInfoResult is returned by chain_getInfo.
type ChainInfoResult struct {
	ChainID string `json:"chain_id"`
	Symbol  string `json:"symbol,omitempty"`
	Height  uint64 `json:"height"`
	TipHash string `json:"tip_hash"`
}

// BalanceResult is returned by utxo_getBalance.
type BalanceResult struct {
	Address   string `json:"address"`
	Balance   uint64 `json:"balance"`   // Total (backwards compat)
	Spendable uint64 `json:"spendable"` // Mature, unlocked, P2PKH, non-token
	Immature  uint64 `json:"immature"`  // Coinbase not yet matured
	Staked    uint64 `json:"staked"`    // ScriptTypeStake UTXOs
	Locked    uint64 `json:"locked"`    // Unstake cooldown (LockedUntil > height)
}

// UTXOListResult is returned by utxo_getByAddress.
type UTXOListResult struct {
	Address string       `json:"address"`
	UTXOs   []*utxo.UTXO `json:"utxos"`
}

// TxSubmitResult is returned by tx_submit.
type TxSubmitResult struct {
	TxHash string `json:"tx_hash"`
}

// TxValidateResult is returned by tx_validate.
type TxValidateResult struct {
	Valid bool   `json:"valid"`
	Fee   uint64 `json:"fee,omitempty"`
	Error string `json:"error,omitempty"`
}

// MempoolInfoResult is returned by mempool_getInfo.
type MempoolInfoResult struct {
	Count  int    `json:"count"`
	MinFeeRate uint64 `json:"min_fee_rate"`
}

// MempoolContentResult is returned by mempool_getContent.
type MempoolContentResult struct {
	Hashes []string `json:"hashes"`
}

// PeerInfo describes a connected peer.
type PeerInfo struct {
	ID          string `json:"id"`
	ConnectedAt string `json:"connected_at"`
}

// PeerInfoResult is returned by net_getPeerInfo.
type PeerInfoResult struct {
	Count int        `json:"count"`
	Peers []PeerInfo `json:"peers"`
}

// NodeInfoResult is returned by net_getNodeInfo.
type NodeInfoResult struct {
	ID    string   `json:"id"`
	Addrs []string `json:"addrs"`
}

// BanEntry describes a single banned peer.
type BanEntry struct {
	ID        string `json:"id"`
	Reason    string `json:"reason"`
	Score     int    `json:"score"`
	BannedAt  int64  `json:"banned_at"`
	ExpiresAt int64  `json:"expires_at"`
}

// BanListResult is returned by net_getBanList.
type BanListResult struct {
	Count int        `json:"count"`
	Bans  []BanEntry `json:"bans"`
}

// PubKeyParam is used by stake endpoints that take a public key.
type PubKeyParam struct {
	PubKey string `json:"pubkey"`
}

// StakeInfoResult is returned by stake_getInfo.
type StakeInfoResult struct {
	PubKey     string `json:"pubkey"`
	TotalStake uint64 `json:"total_stake"`
	MinStake   uint64 `json:"min_stake"`
	Sufficient bool   `json:"sufficient"`
	IsGenesis  bool   `json:"is_genesis"`
}

// ValidatorEntry describes a single validator in the list.
type ValidatorEntry struct {
	PubKey    string `json:"pubkey"`
	IsGenesis bool   `json:"is_genesis"`
}

// ValidatorsResult is returned by stake_getValidators.
type ValidatorsResult struct {
	MinStake   uint64           `json:"min_stake"`
	Validators []ValidatorEntry `json:"validators"`
}

// ChainIDParam is used by sub-chain endpoints that take a chain ID.
type ChainIDParam struct {
	ChainID string `json:"chain_id"`
}

// SubChainInfoResult describes a single sub-chain.
type SubChainInfoResult struct {
	ChainID           string `json:"chain_id"`
	Name              string `json:"name"`
	Symbol            string `json:"symbol"`
	ConsensusType     string `json:"consensus_type"`
	Syncing           bool   `json:"syncing"`
	Height            uint64 `json:"height"`
	TipHash           string `json:"tip_hash"`
	CreatedAt         uint64 `json:"created_at"`
	RegistrationTx    string `json:"registration_tx"`
	InitialDifficulty uint64 `json:"initial_difficulty,omitempty"`
	DifficultyAdjust  int    `json:"difficulty_adjust,omitempty"`
	CurrentDifficulty uint64 `json:"current_difficulty,omitempty"`
}

// SubChainListResult is returned by subchain_list.
type SubChainListResult struct {
	Count  int                  `json:"count"`
	Chains []SubChainInfoResult `json:"chains"`
}

// ── Wallet param types ──────────────────────────────────────────────────

// WalletCreateParam is used by wallet_create.
type WalletCreateParam struct {
	Name     string `json:"name"`
	Password string `json:"password"`
}

// WalletImportParam is used by wallet_import.
type WalletImportParam struct {
	Name     string `json:"name"`
	Password string `json:"password"`
	Mnemonic string `json:"mnemonic"`
}

// WalletUnlockParam is used by endpoints that need wallet name + password.
type WalletUnlockParam struct {
	Name     string `json:"name"`
	Password string `json:"password"`
}

// WalletNewAddressParam is used by wallet_newAddress.
type WalletNewAddressParam struct {
	Name     string `json:"name"`
	Password string `json:"password"`
}

// WalletSendParam is used by wallet_send.
type WalletSendParam struct {
	Name     string `json:"name"`
	Password string `json:"password"`
	To       string `json:"to"`
	Amount   uint64 `json:"amount"`
}

// WalletExportKeyParam is used by wallet_exportKey.
type WalletExportKeyParam struct {
	Name     string `json:"name"`
	Password string `json:"password"`
	Account  uint32 `json:"account"`
	Index    uint32 `json:"index"`
}

// WalletStakeParam is used by wallet_stake.
type WalletStakeParam struct {
	Name     string `json:"name"`
	Password string `json:"password"`
	Amount   uint64 `json:"amount"` // Stake amount in base units.
}

// WalletMintTokenParam is used by wallet_mintToken.
type WalletMintTokenParam struct {
	Name      string `json:"name"`
	Password  string `json:"password"`
	TokenName string `json:"token_name"`
	Symbol    string `json:"token_symbol"`
	Decimals  uint8  `json:"decimals"`
	Amount    uint64 `json:"amount"`
	Recipient string `json:"recipient"` // Optional, defaults to sender.
}

// ── Wallet result types ─────────────────────────────────────────────────

// WalletCreateResult is returned by wallet_create.
type WalletCreateResult struct {
	Mnemonic string `json:"mnemonic"`
	Address  string `json:"address"`
}

// WalletImportResult is returned by wallet_import.
type WalletImportResult struct {
	Address string `json:"address"`
}

// WalletListResult is returned by wallet_list.
type WalletListResult struct {
	Wallets []string `json:"wallets"`
}

// WalletAddressResult is returned by wallet_newAddress.
type WalletAddressResult struct {
	Index   uint32 `json:"index"`
	Address string `json:"address"`
}

// WalletAddressListResult is returned by wallet_listAddresses.
type WalletAddressListResult struct {
	Accounts []WalletAccountEntry `json:"accounts"`
}

// WalletAccountEntry describes a wallet account in RPC results.
type WalletAccountEntry struct {
	Index   uint32 `json:"index"`
	Change  uint32 `json:"change"` // 0=external, 1=internal
	Name    string `json:"name"`
	Address string `json:"address"`
}

// WalletSendResult is returned by wallet_send.
type WalletSendResult struct {
	TxHash string `json:"tx_hash"`
}

// WalletConsolidateParam is used by wallet_consolidate.
type WalletConsolidateParam struct {
	Name      string `json:"name"`
	Password  string `json:"password"`
	MaxInputs uint32 `json:"max_inputs,omitempty"` // Max inputs to merge in one tx (default: 500)
	ChainID   string `json:"chain_id,omitempty"`   // Optional: consolidate on a sub-chain instead of root.
}

// WalletConsolidateResult is returned by wallet_consolidate.
type WalletConsolidateResult struct {
	TxHash       string `json:"tx_hash"`
	ChainID      string `json:"chain_id,omitempty"`
	InputsUsed   uint32 `json:"inputs_used"`
	InputTotal   uint64 `json:"input_total"`
	OutputAmount uint64 `json:"output_amount"`
	Fee          uint64 `json:"fee"`
}

// Recipient is a single output in a sendMany transaction.
type Recipient struct {
	To     string `json:"to"`
	Amount uint64 `json:"amount"`
}

// WalletSendManyParam is used by wallet_sendMany.
type WalletSendManyParam struct {
	Name       string      `json:"name"`
	Password   string      `json:"password"`
	Recipients []Recipient `json:"recipients"`
}

// WalletSendManyResult is returned by wallet_sendMany.
type WalletSendManyResult struct {
	TxHash string `json:"tx_hash"`
}

// WalletExportKeyResult is returned by wallet_exportKey.
type WalletExportKeyResult struct {
	PrivateKey string `json:"private_key"`
	PubKey     string `json:"pubkey"`
	Address    string `json:"address"`
}

// WalletStakeResult is returned by wallet_stake.
type WalletStakeResult struct {
	TxHash string `json:"tx_hash"`
	PubKey string `json:"pubkey"`
}

// WalletMintTokenResult is returned by wallet_mintToken.
type WalletMintTokenResult struct {
	TxHash  string `json:"tx_hash"`
	TokenID string `json:"token_id"`
}

// WalletUnstakeParam is used by wallet_unstake.
type WalletUnstakeParam struct {
	Name     string `json:"name"`
	Password string `json:"password"`
}

// WalletUnstakeResult is returned by wallet_unstake.
type WalletUnstakeResult struct {
	TxHash string `json:"tx_hash"`
	Amount uint64 `json:"amount"`
	PubKey string `json:"pubkey"`
}

// WalletSendTokenParam is used by wallet_sendToken.
type WalletSendTokenParam struct {
	Name     string `json:"name"`
	Password string `json:"password"`
	TokenID  string `json:"token_id"`
	To       string `json:"to"`
	Amount   uint64 `json:"amount"`
}

// WalletSendTokenResult is returned by wallet_sendToken.
type WalletSendTokenResult struct {
	TxHash string `json:"tx_hash"`
}

// ── Sub-chain management param/result types ─────────────────────────────

// WalletCreateSubChainParam is used by wallet_createSubChain.
type WalletCreateSubChainParam struct {
	Name              string   `json:"name"`
	Password          string   `json:"password"`
	ChainName         string   `json:"chain_name"`
	Symbol            string   `json:"symbol"`
	ConsensusType     string   `json:"consensus_type"`
	BlockTime         int      `json:"block_time"`
	BlockReward       uint64   `json:"block_reward"`
	MaxSupply         uint64   `json:"max_supply"`
	MinFeeRate        uint64   `json:"min_fee_rate"`
	Validators        []string `json:"validators,omitempty"`
	InitialDifficulty uint64   `json:"initial_difficulty,omitempty"`
	DifficultyAdjust  int      `json:"difficulty_adjust,omitempty"`
	ValidatorStake    uint64   `json:"validator_stake,omitempty"`
}

// WalletCreateSubChainResult is returned by wallet_createSubChain.
type WalletCreateSubChainResult struct {
	TxHash  string `json:"tx_hash"`
	ChainID string `json:"chain_id"`
}

// SubChainBalanceParam is used by subchain_getBalance.
type SubChainBalanceParam struct {
	ChainID string `json:"chain_id"`
	Address string `json:"address"`
}

// SubChainBalanceResult is returned by subchain_getBalance.
type SubChainBalanceResult struct {
	ChainID   string `json:"chain_id"`
	Address   string `json:"address"`
	Balance   uint64 `json:"balance"`   // Total (backwards compat)
	Spendable uint64 `json:"spendable"` // Mature, unlocked, P2PKH, non-token
	Immature  uint64 `json:"immature"`  // Coinbase not yet matured
	Staked    uint64 `json:"staked"`    // ScriptTypeStake UTXOs
	Locked    uint64 `json:"locked"`    // Unstake cooldown (LockedUntil > height)
}

// SubChainSendParam is used by subchain_send.
type SubChainSendParam struct {
	ChainID  string `json:"chain_id"`
	Name     string `json:"name"`
	Password string `json:"password"`
	To       string `json:"to"`
	Amount   uint64 `json:"amount"`
}

// SubChainSendResult is returned by subchain_send.
type SubChainSendResult struct {
	TxHash string `json:"tx_hash"`
}

// SubChainStakeParam is used by subchain_stake.
type SubChainStakeParam struct {
	ChainID  string `json:"chain_id"`
	Name     string `json:"name"`
	Password string `json:"password"`
	Amount   uint64 `json:"amount"`
}

// SubChainStakeResult is returned by subchain_stake.
type SubChainStakeResult struct {
	TxHash string `json:"tx_hash"`
	PubKey string `json:"pubkey"`
}

// SubChainUnstakeParam is used by subchain_unstake.
type SubChainUnstakeParam struct {
	ChainID  string `json:"chain_id"`
	Name     string `json:"name"`
	Password string `json:"password"`
}

// SubChainUnstakeResult is returned by subchain_unstake.
type SubChainUnstakeResult struct {
	TxHash string `json:"tx_hash"`
	Amount uint64 `json:"amount"`
	PubKey string `json:"pubkey"`
}

// ── Validator status result types ────────────────────────────────────────

// ValidatorStatusResult is returned by validator_getStatus.
type ValidatorStatusResult struct {
	PubKey        string `json:"pubkey"`
	IsOnline      bool   `json:"is_online"`
	LastHeartbeat int64  `json:"last_heartbeat"` // unix timestamp, 0 if never
	LastBlock     int64  `json:"last_block"`     // unix timestamp, 0 if never
	BlockCount    uint64 `json:"block_count"`
	MissedCount   uint64 `json:"missed_count"`
	IsGenesis     bool   `json:"is_genesis"`
}

// ValidatorStatusListResult is returned by validator_getStatus (no params).
type ValidatorStatusListResult struct {
	Validators []ValidatorStatusResult `json:"validators"`
}

// ── Mining param/result types ────────────────────────────────────────────

// MiningGetBlockTemplateParam is used by mining_getBlockTemplate.
type MiningGetBlockTemplateParam struct {
	ChainID         string `json:"chain_id"`
	CoinbaseAddress string `json:"coinbase_address"`
}

// MiningBlockTemplateResult is returned by mining_getBlockTemplate.
type MiningBlockTemplateResult struct {
	Block      *block.Block `json:"block"`      // Full block (nonce=0, ready to mine)
	Target     string       `json:"target"`     // Hex-encoded 256-bit target (hash must be <= this)
	Difficulty uint64       `json:"difficulty"` // Numeric difficulty
	Height     uint64       `json:"height"`     // Block height
	PrevHash   string       `json:"prev_hash"`  // Previous block hash (hex)
}

// MiningSubmitBlockParam is used by mining_submitBlock.
type MiningSubmitBlockParam struct {
	ChainID string       `json:"chain_id"`
	Block   *block.Block `json:"block"`
}

// MiningSubmitBlockResult is returned by mining_submitBlock.
type MiningSubmitBlockResult struct {
	BlockHash string `json:"block_hash"`
	Height    uint64 `json:"height"`
}

// ── Wallet history param/result types ────────────────────────────────────

// WalletGetHistoryParam is used by wallet_getHistory.
type WalletGetHistoryParam struct {
	Name     string `json:"name"`
	Password string `json:"password"`
	Limit    int    `json:"limit,omitempty"`
	Offset   int    `json:"offset,omitempty"`
}

// TxHistoryEntry describes a single transaction in wallet history.
type TxHistoryEntry struct {
	TxHash      string `json:"tx_hash"`
	BlockHash   string `json:"block_hash"`
	Height      uint64 `json:"height"`
	Timestamp   uint64 `json:"timestamp"`
	Type        string `json:"type"`
	Amount      string `json:"amount"`
	Fee         string `json:"fee"`
	To          string `json:"to,omitempty"`
	From        string `json:"from,omitempty"`
	Confirmed   bool   `json:"confirmed"`
	TokenID     string `json:"token_id,omitempty"`     // Token ID (hex) for token_sent / token_received.
	TokenAmount uint64 `json:"token_amount,omitempty"` // Token units transferred.
}

// WalletGetHistoryResult is returned by wallet_getHistory.
type WalletGetHistoryResult struct {
	Total   int              `json:"total"`
	Entries []TxHistoryEntry `json:"entries"`
}

// WalletRescanParam is used by wallet_rescan.
type WalletRescanParam struct {
	Name        string `json:"name"`
	Password    string `json:"password"`
	FromHeight  uint64 `json:"from_height,omitempty"`
	DeriveLimit uint32 `json:"derive_limit,omitempty"` // Optional max address index per chain to derive during scan.
	ChainID     string `json:"chain_id,omitempty"`     // Optional: scan a sub-chain instead of root.
}

// WalletRescanResult is returned by wallet_rescan.
type WalletRescanResult struct {
	AddressesFound int    `json:"addresses_found"`
	AddressesNew   int    `json:"addresses_new"`
	FromHeight     uint64 `json:"from_height"`
	ToHeight       uint64 `json:"to_height"`
}

// ── Token param/result types ────────────────────────────────────────────

// TokenIDParam is used by token_getInfo.
type TokenIDParam struct {
	TokenID string `json:"token_id"`
}

// TokenInfoResult is returned by token_getInfo and used in token_list.
type TokenInfoResult struct {
	TokenID  string `json:"token_id"`
	Name     string `json:"name"`
	Symbol   string `json:"symbol"`
	Decimals uint8  `json:"decimals"`
	Creator  string `json:"creator"`
}

// TokenBalanceEntry describes a token balance for a single token type.
type TokenBalanceEntry struct {
	TokenID  string `json:"token_id"`
	Amount   uint64 `json:"amount"`
	Name     string `json:"name"`
	Symbol   string `json:"symbol"`
	Decimals uint8  `json:"decimals"`
}

// TokenBalanceResult is returned by token_getBalance.
type TokenBalanceResult struct {
	Address string              `json:"address"`
	Tokens  []TokenBalanceEntry `json:"tokens"`
}

// TokenListResult is returned by token_list.
type TokenListResult struct {
	Tokens []TokenInfoResult `json:"tokens"`
}
