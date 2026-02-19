// klingnet-cli is a command-line client for interacting with a klingnetd node.
package main

import (
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Klingon-tech/klingnet-chain/config"
	"github.com/Klingon-tech/klingnet-chain/internal/rpc"
	"github.com/Klingon-tech/klingnet-chain/internal/rpcclient"
	"github.com/Klingon-tech/klingnet-chain/internal/wallet"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
	"golang.org/x/term"
)

// keystoreDir returns the keystore path matching klingnetd's layout:
// <datadir>/<network>/keystore
func keystoreDir(dataDir, network string) string {
	return filepath.Join(dataDir, network, "keystore")
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	// Parse global flags that appear before the subcommand.
	rpcURL := "http://127.0.0.1:8545"
	dataDir := defaultDataDir()
	network := "mainnet"
	chainID := ""

	// Scan for --rpc, --datadir, --network, and --chain-id before the subcommand.
	args := os.Args[1:]
	for len(args) > 0 {
		switch {
		case args[0] == "--rpc" && len(args) > 1:
			rpcURL = args[1]
			args = args[2:]
		case strings.HasPrefix(args[0], "--rpc="):
			rpcURL = args[0][len("--rpc="):]
			args = args[1:]
		case args[0] == "--datadir" && len(args) > 1:
			dataDir = args[1]
			args = args[2:]
		case strings.HasPrefix(args[0], "--datadir="):
			dataDir = args[0][len("--datadir="):]
			args = args[1:]
		case args[0] == "--network" && len(args) > 1:
			network = args[1]
			args = args[2:]
		case strings.HasPrefix(args[0], "--network="):
			network = args[0][len("--network="):]
			args = args[1:]
		case args[0] == "--chain-id" && len(args) > 1:
			chainID = args[1]
			args = args[2:]
		case strings.HasPrefix(args[0], "--chain-id="):
			chainID = args[0][len("--chain-id="):]
			args = args[1:]
		default:
			goto dispatch
		}
	}

dispatch:
	// Set address HRP based on network.
	if network == "testnet" {
		types.SetAddressHRP(types.TestnetHRP)
	} else {
		types.SetAddressHRP(types.MainnetHRP)
	}

	if len(args) == 0 {
		usage()
		os.Exit(1)
	}

	ksDir := keystoreDir(dataDir, network)
	client := rpcclient.New(rpcURL)
	cmd := args[0]
	cmdArgs := args[1:]

	switch cmd {
	case "status":
		cmdStatus(client, chainID)
	case "block":
		cmdBlock(client, cmdArgs, chainID)
	case "tx":
		cmdTx(client, cmdArgs, ksDir, rpcURL, chainID)
	case "send":
		cmdSend(cmdArgs, ksDir, rpcURL, chainID)
	case "sendmany":
		cmdSendMany(cmdArgs, ksDir, rpcURL, chainID)
	case "balance":
		cmdBalance(client, cmdArgs, chainID)
	case "mempool":
		cmdMempool(client, chainID)
	case "peers":
		cmdPeers(client)
	case "wallet":
		cmdWallet(cmdArgs, ksDir, rpcURL)
	case "validators":
		cmdValidators(client)
	case "token":
		cmdToken(client, cmdArgs)
	case "stake":
		cmdStake(client, cmdArgs)
	case "subchains":
		cmdSubChains(client)
	case "subchain":
		cmdSubChain(client, cmdArgs, ksDir, rpcURL, network)
	case "mining":
		cmdMining(client, cmdArgs)
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", cmd)
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `Usage: klingnet-cli [global flags] <command> [flags]

Global flags:
  --rpc <url>         RPC endpoint (default: http://127.0.0.1:8545)
  --datadir <path>    Data directory (default: ~/.klingnet)
  --network <net>     mainnet (default) or testnet
  --chain-id <hex>    Target sub-chain (32-byte hex; omit for root chain)

Commands:
  status                          Show chain status
  block <hash|height>             Show block details
  tx <hash>                       Show transaction details
  send --wallet <w> --to <addr> --amount <amt>
                                  Send a transaction
  sendmany --wallet <w> --recipients <file.json>
                                  Send to multiple recipients (JSON file)
  balance <address>               Show address balance
  mempool                         Show mempool stats
  peers                           Show connected peers

  wallet create --name <n>        Create a new wallet
  wallet import --name <n> --mnemonic "..."
                                  Import wallet from mnemonic
  wallet list                     List wallets
  wallet address --wallet <w>     List wallet addresses
  wallet new-address --wallet <w> Generate a new address
  wallet consolidate --wallet <w> Consolidate many small UTXOs into one
  wallet rescan --wallet <w>      Re-scan chain to discover used addresses
  wallet balance [--wallet <w>]   Show wallet balance(s)
  wallet export-key --wallet <w>  Export private key for validator

  token list                      List all known tokens
  token info <token_id>           Show token metadata
  token balance <address>         Show token balances for address
  token mint --wallet <w> --name <n> --symbol <SYM> --amount <n>
                                  Create a new token (costs 50 KGX)
  token send --wallet <w> --token <id> --to <addr> --amount <n>
                                  Transfer tokens

  validators                      Show validator list
  stake info <pubkey>             Show stake info
  stake create --wallet <w> --amount <amt>
                                  Stake to become a validator
  stake withdraw --wallet <w>     Withdraw all stake

  subchains                       List sub-chains
  subchain info <id>              Show sub-chain details
  subchain create --wallet <w> --name <n> --symbol <SYM> [opts]
                                  Create a sub-chain (burns 1,000 KGX)
  subchain balance <id> <addr>    Show address balance on sub-chain
  subchain send --wallet <w> --chain <id> --to <addr> --amount <n>
                                  Send on a sub-chain

  mining gettemplate --chain <id> --address <coinbase>
                                  Get a PoW block template for external mining
  mining submit --chain <id> --block <json_file>
                                  Submit a solved PoW block
`)
}

func defaultDataDir() string {
	return config.DefaultDataDir()
}

// ── status ──────────────────────────────────────────────────────────────

func cmdStatus(client *rpcclient.Client, chainID string) {
	var params interface{}
	if chainID != "" {
		params = map[string]string{"chain_id": chainID}
	}

	var info rpc.ChainInfoResult
	if err := client.Call("chain_getInfo", params, &info); err != nil {
		fatal("chain_getInfo: %v", err)
	}

	fmt.Printf("Chain:   %s\n", info.ChainID)
	if info.Symbol != "" {
		fmt.Printf("Symbol:  %s\n", info.Symbol)
	}
	fmt.Printf("Height:  %d\n", info.Height)
	fmt.Printf("Tip:     %s\n", info.TipHash)

	// Only show peers for root chain.
	if chainID == "" {
		var peers rpc.PeerInfoResult
		if err := client.Call("net_getPeerInfo", nil, &peers); err != nil {
			fatal("net_getPeerInfo: %v", err)
		}
		fmt.Printf("Peers:   %d\n", peers.Count)
	}
}

// ── block ───────────────────────────────────────────────────────────────

func cmdBlock(client *rpcclient.Client, args []string, chainID string) {
	if len(args) < 1 {
		fatal("Usage: klingnet-cli block <hash|height>")
	}

	arg := args[0]
	var raw json.RawMessage

	// Try as height first (pure number).
	if height, err := strconv.ParseUint(arg, 10, 64); err == nil {
		if err := client.Call("chain_getBlockByHeight", rpc.HeightParam{Height: height, ChainID: chainID}, &raw); err != nil {
			fatal("chain_getBlockByHeight: %v", err)
		}
	} else {
		// Treat as hash.
		if err := client.Call("chain_getBlockByHash", rpc.HashParam{Hash: arg, ChainID: chainID}, &raw); err != nil {
			fatal("chain_getBlockByHash: %v", err)
		}
	}

	// Parse the block to display formatted output.
	var blk struct {
		Header struct {
			PrevHash     string `json:"prev_hash"`
			MerkleRoot   string `json:"merkle_root"`
			Timestamp    uint64 `json:"timestamp"`
			Height       uint64 `json:"height"`
			ValidatorSig string `json:"validator_sig,omitempty"`
		} `json:"header"`
		Transactions []json.RawMessage `json:"transactions"`
	}
	if err := json.Unmarshal(raw, &blk); err != nil {
		fatal("decode block: %v", err)
	}

	fmt.Printf("Height:       %d\n", blk.Header.Height)
	fmt.Printf("Prev:         %s\n", blk.Header.PrevHash)
	fmt.Printf("Merkle Root:  %s\n", blk.Header.MerkleRoot)
	ts := time.Unix(int64(blk.Header.Timestamp), 0).UTC()
	fmt.Printf("Timestamp:    %s\n", ts.Format("2006-01-02 15:04:05 UTC"))
	fmt.Printf("Transactions: %d\n", len(blk.Transactions))
}

// ── tx ──────────────────────────────────────────────────────────────────

func cmdTx(client *rpcclient.Client, args []string, ksDir, rpcURL, chainID string) {
	if len(args) < 1 {
		fatal("Usage: klingnet-cli tx <hash> | send --wallet <w> --to <addr> --amount <amt>")
	}

	if args[0] == "send" {
		// Backward compat: "tx send" works the same as top-level "send".
		cmdSend(args[1:], ksDir, rpcURL, chainID)
		return
	}

	// Show transaction by hash.
	hash := args[0]
	var raw json.RawMessage
	if err := client.Call("chain_getTransaction", rpc.HashParam{Hash: hash, ChainID: chainID}, &raw); err != nil {
		fatal("chain_getTransaction: %v", err)
	}

	var txn struct {
		Version uint32 `json:"version"`
		Inputs  []struct {
			PrevOut struct {
				TxID  string `json:"tx_id"`
				Index uint32 `json:"index"`
			} `json:"prevout"`
		} `json:"inputs"`
		Outputs []struct {
			Value  uint64 `json:"value"`
			Script struct {
				Type uint8  `json:"type"`
				Data string `json:"data"`
			} `json:"script"`
		} `json:"outputs"`
		LockTime uint64 `json:"locktime"`
	}
	if err := json.Unmarshal(raw, &txn); err != nil {
		fatal("decode tx: %v", err)
	}

	fmt.Printf("Version:  %d\n", txn.Version)
	fmt.Printf("LockTime: %d\n", txn.LockTime)
	fmt.Printf("Inputs:   %d\n", len(txn.Inputs))
	for i, in := range txn.Inputs {
		fmt.Printf("  [%d] %s:%d\n", i, in.PrevOut.TxID, in.PrevOut.Index)
	}
	fmt.Printf("Outputs:  %d\n", len(txn.Outputs))
	for i, out := range txn.Outputs {
		fmt.Printf("  [%d] %s -> %s\n", i, formatAmount(out.Value), out.Script.Data)
	}
}

// ── send (top-level) ────────────────────────────────────────────────────

func cmdSend(args []string, ksDir, rpcURL, chainID string) {
	fs := flag.NewFlagSet("send", flag.ExitOnError)
	walletName := fs.String("wallet", "", "Wallet name")
	toAddr := fs.String("to", "", "Recipient address")
	amountStr := fs.String("amount", "", "Amount to send (e.g. 1.5)")
	fs.Parse(args)

	if *walletName == "" || *toAddr == "" || *amountStr == "" {
		fatal("Usage: klingnet-cli send --wallet <name> --to <addr> --amount <amt>")
	}

	// Parse amount.
	amount, err := parseAmount(*amountStr)
	if err != nil {
		fatal("invalid amount: %v", err)
	}

	// Parse recipient address.
	recipientAddr, err := types.ParseAddress(*toAddr)
	if err != nil {
		fatal("invalid recipient address: %v", err)
	}

	// Prompt for password.
	password, err := readPassword("Enter password: ")
	if err != nil {
		fatal("read password: %v", err)
	}

	// Root-chain sends should use wallet_send so all wallet accounts
	// (external + change) are considered during coin selection.
	if chainID == "" {
		client := rpcclient.New(rpcURL)
		var result rpc.WalletSendResult
		if err := client.Call("wallet_send", rpc.WalletSendParam{
			Name:     *walletName,
			Password: string(password),
			To:       *toAddr,
			Amount:   amount,
		}, &result); err != nil {
			fatal("wallet_send: %v", err)
		}
		fmt.Printf("Submitted: %s\n", result.TxHash)
		return
	}

	// Sub-chain sends should use subchain_send so all wallet accounts
	// (external + change) are considered during coin selection.
	client := rpcclient.New(rpcURL)
	var result rpc.SubChainSendResult
	if err := client.Call("subchain_send", rpc.SubChainSendParam{
		ChainID:  chainID,
		Name:     *walletName,
		Password: string(password),
		To:       recipientAddr.String(),
		Amount:   amount,
	}, &result); err != nil {
		fatal("subchain_send: %v", err)
	}
	fmt.Printf("Submitted: %s\n", result.TxHash)
}

// ── sendmany ────────────────────────────────────────────────────────────

func cmdSendMany(args []string, ksDir, rpcURL, chainID string) {
	fs := flag.NewFlagSet("sendmany", flag.ExitOnError)
	walletName := fs.String("wallet", "", "Wallet name")
	recipientsFile := fs.String("recipients", "", "Path to JSON recipients file")
	fs.Parse(args)

	if *walletName == "" || *recipientsFile == "" {
		fatal("Usage: klingnet-cli sendmany --wallet <name> --recipients <file.json>")
	}

	// Read and parse recipients file.
	data, err := os.ReadFile(*recipientsFile)
	if err != nil {
		fatal("read recipients file: %v", err)
	}

	type jsonRecipient struct {
		To     string `json:"to"`
		Amount string `json:"amount"`
	}
	var jsonRecipients []jsonRecipient
	if err := json.Unmarshal(data, &jsonRecipients); err != nil {
		fatal("parse recipients JSON: %v", err)
	}
	if len(jsonRecipients) == 0 {
		fatal("recipients file is empty")
	}

	// Validate and convert recipients.
	recipients := make([]rpc.Recipient, len(jsonRecipients))
	for i, r := range jsonRecipients {
		if r.To == "" || r.Amount == "" {
			fatal("recipient %d: to and amount are required", i)
		}
		if _, err := types.ParseAddress(r.To); err != nil {
			fatal("recipient %d: invalid address: %v", i, err)
		}
		amount, err := parseAmount(r.Amount)
		if err != nil {
			fatal("recipient %d: invalid amount: %v", i, err)
		}
		recipients[i] = rpc.Recipient{To: r.To, Amount: amount}
	}

	// Prompt for password.
	password, err := readPassword("Enter password: ")
	if err != nil {
		fatal("read password: %v", err)
	}

	// Unlock wallet locally to verify password.
	ks, err := wallet.NewKeystore(ksDir)
	if err != nil {
		fatal("open keystore: %v", err)
	}
	if _, err := ks.Load(*walletName, password); err != nil {
		fatal("invalid password or wallet: %v", err)
	}

	// Submit via RPC.
	client := rpcclient.New(rpcURL)
	params := rpc.WalletSendManyParam{
		Name:       *walletName,
		Password:   string(password),
		Recipients: recipients,
	}
	if chainID != "" {
		fatal("sendmany on sub-chains is not yet supported")
	}

	var result rpc.WalletSendManyResult
	if err := client.Call("wallet_sendMany", params, &result); err != nil {
		fatal("wallet_sendMany: %v", err)
	}

	fmt.Printf("Submitted: %s\n", result.TxHash)
	fmt.Printf("Recipients: %d\n", len(recipients))
}

// ── balance ─────────────────────────────────────────────────────────────

func cmdBalance(client *rpcclient.Client, args []string, chainID string) {
	if len(args) < 1 {
		fatal("Usage: klingnet-cli balance <address>")
	}

	addr := args[0]
	var result rpc.BalanceResult
	if err := client.Call("utxo_getBalance", rpc.AddressParam{Address: addr, ChainID: chainID}, &result); err != nil {
		fatal("utxo_getBalance: %v", err)
	}

	fmt.Printf("Address:   %s\n", result.Address)
	fmt.Printf("Spendable: %s KGX\n", formatAmount(result.Spendable))
	if result.Balance != result.Spendable {
		fmt.Printf("Total:     %s KGX\n", formatAmount(result.Balance))
		if result.Immature > 0 {
			fmt.Printf("Immature:  %s KGX\n", formatAmount(result.Immature))
		}
		if result.Staked > 0 {
			fmt.Printf("Staked:    %s KGX\n", formatAmount(result.Staked))
		}
		if result.Locked > 0 {
			fmt.Printf("Locked:    %s KGX\n", formatAmount(result.Locked))
		}
	}
}

// ── mempool ─────────────────────────────────────────────────────────────

func cmdMempool(client *rpcclient.Client, chainID string) {
	var params interface{}
	if chainID != "" {
		params = map[string]string{"chain_id": chainID}
	}

	var info rpc.MempoolInfoResult
	if err := client.Call("mempool_getInfo", params, &info); err != nil {
		fatal("mempool_getInfo: %v", err)
	}

	fmt.Printf("Count:   %d\n", info.Count)
	fmt.Printf("Min Fee Rate: %d per byte\n", info.MinFeeRate)

	if info.Count > 0 {
		var content rpc.MempoolContentResult
		if err := client.Call("mempool_getContent", params, &content); err != nil {
			fatal("mempool_getContent: %v", err)
		}
		fmt.Println("Pending:")
		for _, h := range content.Hashes {
			fmt.Printf("  %s\n", h)
		}
	}
}

// ── peers ───────────────────────────────────────────────────────────────

func cmdPeers(client *rpcclient.Client) {
	var node rpc.NodeInfoResult
	if err := client.Call("net_getNodeInfo", nil, &node); err != nil {
		fatal("net_getNodeInfo: %v", err)
	}

	fmt.Printf("Node ID: %s\n", node.ID)
	for _, a := range node.Addrs {
		fmt.Printf("  Listen: %s\n", a)
	}

	var peers rpc.PeerInfoResult
	if err := client.Call("net_getPeerInfo", nil, &peers); err != nil {
		fatal("net_getPeerInfo: %v", err)
	}

	fmt.Printf("Peers:   %d\n", peers.Count)
	for _, p := range peers.Peers {
		fmt.Printf("  %s (connected: %s)\n", p.ID, p.ConnectedAt)
	}
}

// ── validators ──────────────────────────────────────────────────────────

func cmdValidators(client *rpcclient.Client) {
	var result rpc.ValidatorsResult
	if err := client.Call("stake_getValidators", nil, &result); err != nil {
		fatal("stake_getValidators: %v", err)
	}

	fmt.Printf("Min Stake: %s\n", formatAmount(result.MinStake))
	fmt.Printf("Validators: %d\n\n", len(result.Validators))
	for i, v := range result.Validators {
		genesis := ""
		if v.IsGenesis {
			genesis = " (genesis)"
		}
		fmt.Printf("  [%d] %s%s\n", i, v.PubKey, genesis)
	}
}

// ── stake ───────────────────────────────────────────────────────────────

func cmdStake(client *rpcclient.Client, args []string) {
	if len(args) < 1 {
		fatal("Usage: klingnet-cli stake <info|create|withdraw> [flags]")
	}

	switch args[0] {
	case "info":
		if len(args) < 2 {
			fatal("Usage: klingnet-cli stake info <pubkey>")
		}
		cmdStakeInfo(client, args[1])
	case "create":
		cmdStakeCreate(client, args[1:])
	case "withdraw":
		cmdStakeWithdraw(client, args[1:])
	default:
		fatal("Unknown stake command: %s\nUsage: klingnet-cli stake <info|create|withdraw> [flags]", args[0])
	}
}

func cmdStakeInfo(client *rpcclient.Client, pubkey string) {
	var result rpc.StakeInfoResult
	if err := client.Call("stake_getInfo", rpc.PubKeyParam{PubKey: pubkey}, &result); err != nil {
		fatal("stake_getInfo: %v", err)
	}

	fmt.Printf("PubKey:     %s\n", result.PubKey)
	fmt.Printf("Is Genesis: %v\n", result.IsGenesis)
	fmt.Printf("Total Stake: %s\n", formatAmount(result.TotalStake))
	fmt.Printf("Min Stake:   %s\n", formatAmount(result.MinStake))
	fmt.Printf("Sufficient:  %v\n", result.Sufficient)
}

func cmdStakeCreate(client *rpcclient.Client, args []string) {
	fs := flag.NewFlagSet("stake create", flag.ExitOnError)
	walletName := fs.String("wallet", "", "Wallet name")
	amountStr := fs.String("amount", "", "Stake amount (e.g. 1000)")
	fs.Parse(args)

	if *walletName == "" || *amountStr == "" {
		fatal("Usage: klingnet-cli stake create --wallet <name> --amount <amt>")
	}

	amount, err := parseAmount(*amountStr)
	if err != nil {
		fatal("invalid amount: %v", err)
	}

	// Prompt for password.
	password, err := readPassword("Enter password: ")
	if err != nil {
		fatal("read password: %v", err)
	}

	var result rpc.WalletStakeResult
	if err := client.Call("wallet_stake", rpc.WalletStakeParam{
		Name:     *walletName,
		Password: string(password),
		Amount:   amount,
	}, &result); err != nil {
		fatal("wallet_stake: %v", err)
	}

	fmt.Printf("Stake transaction submitted!\n")
	fmt.Printf("  Tx Hash: %s\n", result.TxHash)
	fmt.Printf("  PubKey:  %s\n", result.PubKey)
	fmt.Printf("  Amount:  %s KGX\n", formatAmount(amount))
	fmt.Println("\nThe validator will be registered when this tx is included in a block.")
	fmt.Println("Use 'klingnet-cli stake info <pubkey>' to check status after confirmation.")
}

func cmdStakeWithdraw(client *rpcclient.Client, args []string) {
	fs := flag.NewFlagSet("stake withdraw", flag.ExitOnError)
	walletName := fs.String("wallet", "", "Wallet name")
	fs.Parse(args)

	if *walletName == "" {
		fatal("Usage: klingnet-cli stake withdraw --wallet <name>")
	}

	// Prompt for password.
	password, err := readPassword("Enter password: ")
	if err != nil {
		fatal("read password: %v", err)
	}

	var result rpc.WalletUnstakeResult
	if err := client.Call("wallet_unstake", rpc.WalletUnstakeParam{
		Name:     *walletName,
		Password: string(password),
	}, &result); err != nil {
		fatal("wallet_unstake: %v", err)
	}

	fmt.Printf("Unstake transaction submitted!\n")
	fmt.Printf("  Tx Hash:  %s\n", result.TxHash)
	fmt.Printf("  PubKey:   %s\n", result.PubKey)
	fmt.Printf("  Returned: %s KGX\n", formatAmount(result.Amount))
	fmt.Println("\nThe validator will be removed when this tx is included in a block.")
	fmt.Println("Returned coins are locked for 20 blocks before they can be spent.")
}

// ── token ───────────────────────────────────────────────────────────────

func cmdToken(client *rpcclient.Client, args []string) {
	if len(args) < 1 {
		fatal("Usage: klingnet-cli token <list|info|balance|mint|send> [flags]")
	}

	switch args[0] {
	case "list":
		cmdTokenList(client)
	case "info":
		if len(args) < 2 {
			fatal("Usage: klingnet-cli token info <token_id>")
		}
		cmdTokenInfo(client, args[1])
	case "balance":
		if len(args) < 2 {
			fatal("Usage: klingnet-cli token balance <address>")
		}
		cmdTokenBalance(client, args[1])
	case "mint":
		cmdTokenMint(client, args[1:])
	case "send":
		cmdTokenSend(client, args[1:])
	default:
		fatal("Unknown token command: %s\nUsage: klingnet-cli token <list|info|balance|mint|send> [flags]", args[0])
	}
}

func cmdTokenList(client *rpcclient.Client) {
	var result rpc.TokenListResult
	if err := client.Call("token_list", nil, &result); err != nil {
		fatal("token_list: %v", err)
	}

	if len(result.Tokens) == 0 {
		fmt.Println("No tokens found.")
		return
	}

	fmt.Printf("Tokens: %d\n\n", len(result.Tokens))
	for i, t := range result.Tokens {
		fmt.Printf("  [%d] %s (%s)\n", i, t.Name, t.Symbol)
		fmt.Printf("      ID:       %s\n", t.TokenID)
		fmt.Printf("      Decimals: %d\n", t.Decimals)
		if t.Creator != "" {
			fmt.Printf("      Creator:  %s\n", t.Creator)
		}
		fmt.Println()
	}
}

func cmdTokenInfo(client *rpcclient.Client, tokenID string) {
	var result rpc.TokenInfoResult
	if err := client.Call("token_getInfo", rpc.TokenIDParam{TokenID: tokenID}, &result); err != nil {
		fatal("token_getInfo: %v", err)
	}

	fmt.Printf("Token ID: %s\n", result.TokenID)
	fmt.Printf("Name:     %s\n", result.Name)
	fmt.Printf("Symbol:   %s\n", result.Symbol)
	fmt.Printf("Decimals: %d\n", result.Decimals)
	fmt.Printf("Creator:  %s\n", result.Creator)
}

func cmdTokenBalance(client *rpcclient.Client, address string) {
	var result rpc.TokenBalanceResult
	if err := client.Call("token_getBalance", rpc.AddressParam{Address: address}, &result); err != nil {
		fatal("token_getBalance: %v", err)
	}

	fmt.Printf("Address: %s\n", result.Address)
	if len(result.Tokens) == 0 {
		fmt.Println("No token balances.")
		return
	}

	fmt.Printf("Tokens: %d\n\n", len(result.Tokens))
	for _, t := range result.Tokens {
		label := t.TokenID[:16] + "..."
		if t.Symbol != "" {
			label = fmt.Sprintf("%s (%s)", t.Symbol, t.TokenID[:16]+"...")
		}
		fmt.Printf("  %s: %d\n", label, t.Amount)
	}
}

func cmdTokenMint(client *rpcclient.Client, args []string) {
	fs := flag.NewFlagSet("token mint", flag.ExitOnError)
	walletName := fs.String("wallet", "", "Wallet name")
	tokenName := fs.String("name", "", "Token name (1-64 chars)")
	symbol := fs.String("symbol", "", "Token symbol (2-10 chars, uppercase)")
	decimals := fs.Uint("decimals", 0, "Decimal places (0-18)")
	amountStr := fs.String("amount", "", "Initial supply (integer token units)")
	recipient := fs.String("to", "", "Recipient address (default: sender)")
	fs.Parse(args)

	if *walletName == "" || *tokenName == "" || *symbol == "" || *amountStr == "" {
		fmt.Fprintf(os.Stderr, `Usage: klingnet-cli token mint [flags]

Required:
  --wallet <name>     Wallet to pay creation fee from
  --name <token_name> Token name (1-64 chars)
  --symbol <SYM>      Token symbol (2-10 uppercase chars)
  --amount <n>        Initial supply (integer token units)

Optional:
  --decimals <n>      Decimal places, 0-18 (default: 0)
  --to <address>      Recipient address (default: sender)

Note: Minting costs a creation fee of 50 KGX.
`)
		os.Exit(1)
	}

	amount, err := strconv.ParseUint(*amountStr, 10, 64)
	if err != nil {
		fatal("invalid amount: %v", err)
	}
	if amount > config.MaxTokenAmount {
		fatal("amount exceeds maximum (%d)", config.MaxTokenAmount)
	}

	// Prompt for password.
	password, err := readPassword("Enter password: ")
	if err != nil {
		fatal("read password: %v", err)
	}

	var result rpc.WalletMintTokenResult
	if err := client.Call("wallet_mintToken", rpc.WalletMintTokenParam{
		Name:      *walletName,
		Password:  string(password),
		TokenName: *tokenName,
		Symbol:    *symbol,
		Decimals:  uint8(*decimals),
		Amount:    amount,
		Recipient: *recipient,
	}, &result); err != nil {
		fatal("wallet_mintToken: %v", err)
	}

	fmt.Printf("Token minted!\n")
	fmt.Printf("  Tx Hash:  %s\n", result.TxHash)
	fmt.Printf("  Token ID: %s\n", result.TokenID)
	fmt.Printf("  Name:     %s\n", *tokenName)
	fmt.Printf("  Symbol:   %s\n", *symbol)
	fmt.Printf("  Amount:   %s\n", *amountStr)
	fmt.Println("\nThe token will be available when this tx is included in a block.")
	fmt.Println("Use 'klingnet-cli token info <token_id>' to check status.")
}

func cmdTokenSend(client *rpcclient.Client, args []string) {
	fs := flag.NewFlagSet("token send", flag.ExitOnError)
	walletName := fs.String("wallet", "", "Wallet name")
	tokenID := fs.String("token", "", "Token ID (32-byte hex)")
	toAddr := fs.String("to", "", "Recipient address")
	amountStr := fs.String("amount", "", "Token amount to send")
	fs.Parse(args)

	if *walletName == "" || *tokenID == "" || *toAddr == "" || *amountStr == "" {
		fatal("Usage: klingnet-cli token send --wallet <name> --token <id> --to <addr> --amount <n>")
	}

	amount, err := strconv.ParseUint(*amountStr, 10, 64)
	if err != nil {
		fatal("invalid amount: %v", err)
	}

	// Prompt for password.
	password, err := readPassword("Enter password: ")
	if err != nil {
		fatal("read password: %v", err)
	}

	var result rpc.WalletSendTokenResult
	if err := client.Call("wallet_sendToken", rpc.WalletSendTokenParam{
		Name:     *walletName,
		Password: string(password),
		TokenID:  *tokenID,
		To:       *toAddr,
		Amount:   amount,
	}, &result); err != nil {
		fatal("wallet_sendToken: %v", err)
	}

	fmt.Printf("Token transfer submitted!\n")
	fmt.Printf("  Tx Hash:  %s\n", result.TxHash)
	fmt.Printf("  Token ID: %s\n", *tokenID)
	fmt.Printf("  Amount:   %d\n", amount)
	fmt.Printf("  To:       %s\n", *toAddr)
}

// ── subchains ───────────────────────────────────────────────────────────

func cmdSubChains(client *rpcclient.Client) {
	var result rpc.SubChainListResult
	if err := client.Call("subchain_list", nil, &result); err != nil {
		fatal("subchain_list: %v", err)
	}

	fmt.Printf("Sub-chains: %d\n\n", result.Count)
	for i, sc := range result.Chains {
		syncStatus := "not syncing"
		if sc.Syncing {
			syncStatus = "syncing"
		}
		fmt.Printf("  [%d] %s (%s) [%s]\n", i, sc.Name, sc.Symbol, syncStatus)
		fmt.Printf("      Chain ID:   %s\n", sc.ChainID)
		fmt.Printf("      Consensus:  %s\n", sc.ConsensusType)
		if sc.Syncing {
			fmt.Printf("      Height:     %d\n", sc.Height)
		}
		if sc.ConsensusType == "pow" {
			fmt.Printf("      Difficulty: %s", formatDifficulty(sc.CurrentDifficulty))
			if sc.CurrentDifficulty != sc.InitialDifficulty {
				fmt.Printf(" (initial: %s)", formatDifficulty(sc.InitialDifficulty))
			}
			fmt.Println()
		}
		fmt.Printf("      Created at: %d\n", sc.CreatedAt)
		fmt.Println()
	}
}

func cmdSubChain(client *rpcclient.Client, args []string, ksDir, rpcURL, network string) {
	if len(args) < 1 {
		fatal("Usage: klingnet-cli subchain <info|create|balance|send|stake|unstake> [flags]")
	}

	switch args[0] {
	case "info":
		if len(args) < 2 {
			fatal("Usage: klingnet-cli subchain info <chain_id>")
		}
		cmdSubChainInfo(client, args[1])
	case "create":
		cmdSubChainCreate(args[1:], ksDir, rpcURL, network)
	case "balance":
		cmdSubChainBalance(client, args[1:])
	case "send":
		cmdSubChainSend(args[1:], rpcURL)
	case "stake":
		cmdSubChainStake(args[1:], rpcURL)
	case "unstake":
		cmdSubChainUnstake(args[1:], rpcURL)
	default:
		fatal("Unknown subchain command: %s\nUsage: klingnet-cli subchain <info|create|balance|send|stake|unstake> [flags]", args[0])
	}
}

func cmdSubChainInfo(client *rpcclient.Client, chainID string) {
	var result rpc.SubChainInfoResult
	if err := client.Call("subchain_getInfo", rpc.ChainIDParam{ChainID: chainID}, &result); err != nil {
		fatal("subchain_getInfo: %v", err)
	}

	fmt.Printf("Chain ID:        %s\n", result.ChainID)
	fmt.Printf("Name:            %s\n", result.Name)
	fmt.Printf("Symbol:          %s\n", result.Symbol)
	fmt.Printf("Consensus:       %s\n", result.ConsensusType)
	if result.Syncing {
		fmt.Printf("Syncing:         yes\n")
		fmt.Printf("Height:          %d\n", result.Height)
		fmt.Printf("Tip:             %s\n", result.TipHash)
	} else {
		fmt.Printf("Syncing:         no (not tracked by this node)\n")
	}
	if result.ConsensusType == "pow" {
		fmt.Printf("Difficulty:      %s (initial: %s)\n", formatDifficulty(result.CurrentDifficulty), formatDifficulty(result.InitialDifficulty))
		if result.DifficultyAdjust > 0 {
			fmt.Printf("Adjust interval: every %d blocks\n", result.DifficultyAdjust)
		} else {
			fmt.Printf("Adjust interval: disabled\n")
		}
	}
	fmt.Printf("Created at:      %d\n", result.CreatedAt)
	fmt.Printf("Registration Tx: %s\n", result.RegistrationTx)
}

func cmdSubChainCreate(args []string, ksDir, rpcURL, network string) {
	fs := flag.NewFlagSet("subchain create", flag.ExitOnError)
	walletName := fs.String("wallet", "", "Wallet name")
	chainName := fs.String("name", "", "Sub-chain name (1-64 chars, alphanumeric/space/hyphen)")
	symbol := fs.String("symbol", "", "Token symbol (2-10 chars, uppercase)")
	consensusType := fs.String("consensus", "poa", "Consensus type: poa or pow")
	blockTimeFlag := fs.Int("block-time", 3, "Target block time in seconds")
	blockRewardStr := fs.String("block-reward", "0.001", "Block reward per block (in coins)")
	maxSupplyStr := fs.String("max-supply", "1000000", "Maximum token supply (in coins)")
	minFeeRateStr := fs.String("min-fee-rate", "10000", "Minimum fee rate (base units per byte)")
	// Burn amount is not customizable — it is a protocol constant.
	// We default to 1,000 KGX (mainnet). On testnet it's 1 KGX.
	// The chain/mempool will reject if the amount doesn't match.
	difficulty := fs.Uint64("difficulty", 1000, "Initial PoW difficulty (pow only)")
	difficultyAdjust := fs.Int("difficulty-adjust", 0, "Blocks between difficulty adjustments (pow only, 0=disabled, min 10)")
	validatorList := fs.String("validators", "", "Comma-separated validator pubkeys hex (poa only)")
	validatorStakeStr := fs.String("validator-stake", "0", "Min stake for dynamic validators (poa only, 0=disabled)")
	fs.Parse(args)

	if *walletName == "" || *chainName == "" || *symbol == "" {
		fmt.Fprintf(os.Stderr, `Usage: klingnet-cli subchain create [flags]

Required:
  --wallet <name>       Wallet to pay burn fee from
  --name <chain_name>   Sub-chain name (1-64 chars)
  --symbol <SYM>        Token symbol (2-10 uppercase chars)

Optional:
  --consensus <type>    poa (default) or pow
  --block-time <secs>   Block time in seconds (default: 3)
  --block-reward <amt>  Block reward in sub-chain coins (default: 0.001)
  --max-supply <amt>    Max supply in sub-chain coins (default: 1000000)
  --min-fee <amt>       Min tx fee in sub-chain coins (default: 0.000001)
  --validators <keys>   Comma-separated pubkey hex (poa only)
  --validator-stake <amt> Min stake for dynamic validators (poa only, 0=disabled)
  --difficulty <n>      Initial difficulty (pow only, default: 1000)
  --difficulty-adjust <n> Blocks between adjustments (pow only, 0=disabled, min 10)

Note: Registration burn amount is a fixed protocol constant (1,000 KGX mainnet, 1 KGX testnet).
`)
		os.Exit(1)
	}

	// Parse amounts.
	blockReward, err := parseAmount(*blockRewardStr)
	if err != nil {
		fatal("invalid block-reward: %v", err)
	}
	maxSupply, err := parseAmount(*maxSupplyStr)
	if err != nil {
		fatal("invalid max-supply: %v", err)
	}
	minFeeRate, err := strconv.ParseUint(*minFeeRateStr, 10, 64)
	if err != nil {
		fatal("invalid min-fee-rate: %v", err)
	}
	validatorStake, err := parseAmount(*validatorStakeStr)
	if err != nil {
		fatal("invalid validator-stake: %v", err)
	}

	var validators []string
	var initialDifficulty uint64
	var adjust int
	var validatorStakeParam uint64
	switch *consensusType {
	case "poa":
		if *validatorList == "" {
			fatal("--validators required for poa consensus (comma-separated 33-byte compressed pubkey hex)")
		}
		validators = strings.Split(*validatorList, ",")
		validatorStakeParam = validatorStake
	case "pow":
		initialDifficulty = *difficulty
		adjust = *difficultyAdjust
	default:
		fatal("--consensus must be 'poa' or 'pow'")
	}

	// Prompt for password.
	password, err := readPassword("Enter password: ")
	if err != nil {
		fatal("read password: %v", err)
	}

	client := rpcclient.New(rpcURL)
	var result rpc.WalletCreateSubChainResult
	if err := client.Call("wallet_createSubChain", rpc.WalletCreateSubChainParam{
		Name:              *walletName,
		Password:          string(password),
		ChainName:         *chainName,
		Symbol:            *symbol,
		ConsensusType:     *consensusType,
		BlockTime:         *blockTimeFlag,
		BlockReward:       blockReward,
		MaxSupply:         maxSupply,
		MinFeeRate:        minFeeRate,
		Validators:        validators,
		InitialDifficulty: initialDifficulty,
		DifficultyAdjust:  adjust,
		ValidatorStake:    validatorStakeParam,
	}, &result); err != nil {
		fatal("wallet_createSubChain: %v", err)
	}

	fmt.Printf("Sub-chain registration submitted!\n")
	fmt.Printf("  Tx Hash:   %s\n", result.TxHash)
	fmt.Printf("  Chain ID:  %s\n", result.ChainID)
	fmt.Printf("  Name:      %s\n", *chainName)
	fmt.Printf("  Symbol:    %s\n", *symbol)
	fmt.Printf("  Consensus: %s\n", *consensusType)
	fmt.Println("\nThe sub-chain will be created when this tx is included in a block.")
	fmt.Println("Use 'klingnet-cli subchains' to check status after confirmation.")
}

func cmdSubChainBalance(client *rpcclient.Client, args []string) {
	if len(args) < 2 {
		fatal("Usage: klingnet-cli subchain balance <chain_id> <address>")
	}

	chainID := args[0]
	address := args[1]

	var result rpc.SubChainBalanceResult
	if err := client.Call("subchain_getBalance", rpc.SubChainBalanceParam{
		ChainID: chainID,
		Address: address,
	}, &result); err != nil {
		fatal("subchain_getBalance: %v", err)
	}

	fmt.Printf("Chain ID: %s\n", result.ChainID)
	fmt.Printf("Address:  %s\n", result.Address)
	fmt.Printf("Balance:  %s\n", formatAmount(result.Balance))
}

func cmdSubChainSend(args []string, rpcURL string) {
	fs := flag.NewFlagSet("subchain send", flag.ExitOnError)
	walletName := fs.String("wallet", "", "Wallet name")
	chainIDFlag := fs.String("chain", "", "Sub-chain ID (32-byte hex)")
	to := fs.String("to", "", "Recipient address")
	amountStr := fs.String("amount", "", "Amount to send (in coins)")
	fs.Parse(args)

	if *walletName == "" || *chainIDFlag == "" || *to == "" || *amountStr == "" {
		fmt.Fprintf(os.Stderr, `Usage: klingnet-cli subchain send [flags]

Required:
  --wallet <name>    Wallet to send from
  --chain <hex>      Sub-chain ID (32-byte hex)
  --to <address>     Recipient address
  --amount <amt>     Amount to send (in coins)
`)
		os.Exit(1)
	}

	amount, err := parseAmount(*amountStr)
	if err != nil {
		fatal("invalid amount: %v", err)
	}

	// Prompt for password.
	password, err := readPassword("Enter password: ")
	if err != nil {
		fatal("read password: %v", err)
	}

	client := rpcclient.New(rpcURL)
	var result rpc.SubChainSendResult
	if err := client.Call("subchain_send", rpc.SubChainSendParam{
		ChainID:  *chainIDFlag,
		Name:     *walletName,
		Password: string(password),
		To:       *to,
		Amount:   amount,
	}, &result); err != nil {
		fatal("subchain_send: %v", err)
	}

	fmt.Printf("Transaction sent on sub-chain!\n")
	fmt.Printf("  Tx Hash:  %s\n", result.TxHash)
	fmt.Printf("  Chain ID: %s\n", *chainIDFlag)
	fmt.Printf("  To:       %s\n", *to)
	fmt.Printf("  Amount:   %s\n", formatAmount(amount))
}

func cmdSubChainStake(args []string, rpcURL string) {
	fs := flag.NewFlagSet("subchain stake", flag.ExitOnError)
	walletName := fs.String("wallet", "", "Wallet name")
	chainIDFlag := fs.String("chain", "", "Sub-chain ID (32-byte hex)")
	amountStr := fs.String("amount", "", "Stake amount (in coins)")
	fs.Parse(args)

	if *walletName == "" || *chainIDFlag == "" || *amountStr == "" {
		fmt.Fprintf(os.Stderr, `Usage: klingnet-cli subchain stake [flags]

Required:
  --wallet <name>    Wallet to stake from
  --chain <hex>      Sub-chain ID (32-byte hex)
  --amount <amt>     Stake amount (in coins)
`)
		os.Exit(1)
	}

	amount, err := parseAmount(*amountStr)
	if err != nil {
		fatal("invalid amount: %v", err)
	}

	// Prompt for password.
	password, err := readPassword("Enter password: ")
	if err != nil {
		fatal("read password: %v", err)
	}

	client := rpcclient.New(rpcURL)
	var result rpc.SubChainStakeResult
	if err := client.Call("subchain_stake", rpc.SubChainStakeParam{
		ChainID:  *chainIDFlag,
		Name:     *walletName,
		Password: string(password),
		Amount:   amount,
	}, &result); err != nil {
		fatal("subchain_stake: %v", err)
	}

	fmt.Printf("Stake transaction submitted on sub-chain!\n")
	fmt.Printf("  Tx Hash:  %s\n", result.TxHash)
	fmt.Printf("  PubKey:   %s\n", result.PubKey)
	fmt.Printf("  Chain ID: %s\n", *chainIDFlag)
	fmt.Printf("  Amount:   %s\n", formatAmount(amount))
	fmt.Println("\nThe validator will be registered when this tx is included in a block.")
}

func cmdSubChainUnstake(args []string, rpcURL string) {
	fs := flag.NewFlagSet("subchain unstake", flag.ExitOnError)
	walletName := fs.String("wallet", "", "Wallet name")
	chainIDFlag := fs.String("chain", "", "Sub-chain ID (32-byte hex)")
	fs.Parse(args)

	if *walletName == "" || *chainIDFlag == "" {
		fmt.Fprintf(os.Stderr, `Usage: klingnet-cli subchain unstake [flags]

Required:
  --wallet <name>    Wallet to unstake from
  --chain <hex>      Sub-chain ID (32-byte hex)
`)
		os.Exit(1)
	}

	// Prompt for password.
	password, err := readPassword("Enter password: ")
	if err != nil {
		fatal("read password: %v", err)
	}

	client := rpcclient.New(rpcURL)
	var result rpc.SubChainUnstakeResult
	if err := client.Call("subchain_unstake", rpc.SubChainUnstakeParam{
		ChainID:  *chainIDFlag,
		Name:     *walletName,
		Password: string(password),
	}, &result); err != nil {
		fatal("subchain_unstake: %v", err)
	}

	fmt.Printf("Unstake transaction submitted on sub-chain!\n")
	fmt.Printf("  Tx Hash:  %s\n", result.TxHash)
	fmt.Printf("  PubKey:   %s\n", result.PubKey)
	fmt.Printf("  Returned: %s\n", formatAmount(result.Amount))
	fmt.Printf("  Chain ID: %s\n", *chainIDFlag)
	fmt.Println("\nThe validator will be removed when this tx is included in a block.")
}

// ── wallet ──────────────────────────────────────────────────────────────

func cmdWallet(args []string, ksDir, rpcURL string) {
	if len(args) < 1 {
		fatal("Usage: klingnet-cli wallet <create|import|list|address|new-address|consolidate|balance|export-key|rescan> [flags]")
	}

	switch args[0] {
	case "create":
		cmdWalletCreate(args[1:], ksDir)
	case "import":
		cmdWalletImport(args[1:], ksDir, rpcURL)
	case "list":
		cmdWalletList(ksDir)
	case "address":
		cmdWalletAddress(args[1:], ksDir)
	case "new-address":
		cmdWalletNewAddress(args[1:], ksDir)
	case "consolidate":
		cmdWalletConsolidate(args[1:], rpcURL)
	case "balance":
		cmdWalletBalance(args[1:], ksDir, rpcURL)
	case "export-key":
		cmdWalletExportKey(args[1:], ksDir)
	case "rescan":
		cmdWalletRescan(args[1:], rpcURL)
	default:
		fatal("Unknown wallet command: %s\nUsage: klingnet-cli wallet <create|import|list|address|new-address|consolidate|balance|export-key|rescan> [flags]", args[0])
	}
}

func cmdWalletCreate(args []string, ksDir string) {
	fs := flag.NewFlagSet("wallet create", flag.ExitOnError)
	name := fs.String("name", "", "Wallet name")
	fs.Parse(args)

	if *name == "" {
		fatal("Usage: klingnet-cli wallet create --name <name>")
	}

	// Generate mnemonic.
	mnemonic, err := wallet.GenerateMnemonic()
	if err != nil {
		fatal("generate mnemonic: %v", err)
	}

	fmt.Println("Mnemonic (write this down!):")
	fmt.Printf("  %s\n\n", mnemonic)

	// Prompt for password (twice).
	password, err := readPassword("Enter password: ")
	if err != nil {
		fatal("read password: %v", err)
	}
	confirm, err := readPassword("Confirm password: ")
	if err != nil {
		fatal("read password: %v", err)
	}
	if string(password) != string(confirm) {
		fatal("passwords do not match")
	}

	// Derive seed.
	seed, err := wallet.SeedFromMnemonic(mnemonic, "")
	if err != nil {
		fatal("derive seed: %v", err)
	}

	// Derive account 0 address before encrypting.
	master, err := wallet.NewMasterKey(seed)
	if err != nil {
		fatal("derive master key: %v", err)
	}
	hdKey, err := master.DeriveAddress(0, wallet.ChangeExternal, 0)
	if err != nil {
		fatal("derive address: %v", err)
	}
	addr := hdKey.Address()

	// Create keystore and save.
	ks, err := wallet.NewKeystore(ksDir)
	if err != nil {
		fatal("create keystore: %v", err)
	}

	if err := ks.Create(*name, seed, password, wallet.DefaultParams()); err != nil {
		fatal("create wallet: %v", err)
	}

	// Zero seed.
	for i := range seed {
		seed[i] = 0
	}

	// Store account 0 metadata.
	if err := ks.AddAccount(*name, wallet.AccountEntry{
		Index:   0,
		Name:    "Default",
		Address: addr.String(),
	}); err != nil {
		fatal("add account: %v", err)
	}

	fmt.Printf("\nWallet created: %s\n", *name)
	fmt.Printf("Address: %s\n", addr.String())
}

func cmdWalletImport(args []string, ksDir, rpcURL string) {
	fs := flag.NewFlagSet("wallet import", flag.ExitOnError)
	name := fs.String("name", "", "Wallet name")
	mnemonic := fs.String("mnemonic", "", "BIP-39 mnemonic (24 words)")
	fs.Parse(args)

	if *name == "" || *mnemonic == "" {
		fatal("Usage: klingnet-cli wallet import --name <name> --mnemonic \"word1 word2 ...\"")
	}

	if !wallet.ValidateMnemonic(*mnemonic) {
		fatal("invalid mnemonic")
	}

	// Prompt for password (twice).
	password, err := readPassword("Enter password: ")
	if err != nil {
		fatal("read password: %v", err)
	}
	confirm, err := readPassword("Confirm password: ")
	if err != nil {
		fatal("read password: %v", err)
	}
	if string(password) != string(confirm) {
		fatal("passwords do not match")
	}

	// Derive seed.
	seed, err := wallet.SeedFromMnemonic(*mnemonic, "")
	if err != nil {
		fatal("derive seed: %v", err)
	}

	// Derive account 0 address.
	master, err := wallet.NewMasterKey(seed)
	if err != nil {
		fatal("derive master key: %v", err)
	}
	hdKey, err := master.DeriveAddress(0, wallet.ChangeExternal, 0)
	if err != nil {
		fatal("derive address: %v", err)
	}
	addr := hdKey.Address()

	// Create keystore and save.
	ks, err := wallet.NewKeystore(ksDir)
	if err != nil {
		fatal("create keystore: %v", err)
	}

	if err := ks.Create(*name, seed, password, wallet.DefaultParams()); err != nil {
		fatal("create wallet: %v", err)
	}

	// Zero seed.
	for i := range seed {
		seed[i] = 0
	}

	// Store account 0 metadata.
	if err := ks.AddAccount(*name, wallet.AccountEntry{
		Index:   0,
		Name:    "Default",
		Address: addr.String(),
	}); err != nil {
		fatal("add account: %v", err)
	}

	fmt.Printf("Wallet imported: %s\n", *name)
	fmt.Printf("Address: %s\n", addr.String())

	// Scan for previously used addresses via RPC (requires running node).
	client := rpcclient.NewWithTimeout(rpcURL, 600*time.Second)
	var result rpc.WalletRescanResult
	if err := client.Call("wallet_rescan", rpc.WalletRescanParam{
		Name:     *name,
		Password: string(password),
	}, &result); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: address scan failed (is node running with --wallet?): %v\n", err)
		fmt.Fprintln(os.Stderr, "Run 'klingnet-cli wallet rescan --wallet "+*name+"' when the node is available.")
		return
	}

	fmt.Printf("Scanned blocks 0-%d: found %d addresses (%d new)\n",
		result.ToHeight, result.AddressesFound, result.AddressesNew)

	// Warn if the node may still be syncing (low height = incomplete scan).
	if result.ToHeight < 10 && result.AddressesFound <= 1 {
		fmt.Fprintln(os.Stderr, "Warning: node appears to still be syncing. Run 'klingnet-cli wallet rescan --wallet "+*name+"' after sync completes.")
	}
}

func cmdWalletList(ksDir string) {
	ks, err := wallet.NewKeystore(ksDir)
	if err != nil {
		fatal("open keystore: %v", err)
	}

	names, err := ks.List()
	if err != nil {
		fatal("list wallets: %v", err)
	}

	if len(names) == 0 {
		fmt.Println("No wallets found.")
		return
	}

	for _, name := range names {
		fmt.Println(name)
	}
}

func cmdWalletAddress(args []string, ksDir string) {
	fs := flag.NewFlagSet("wallet address", flag.ExitOnError)
	walletName := fs.String("wallet", "", "Wallet name")
	fs.Parse(args)

	if *walletName == "" {
		fatal("Usage: klingnet-cli wallet address --wallet <name>")
	}

	ks, err := wallet.NewKeystore(ksDir)
	if err != nil {
		fatal("open keystore: %v", err)
	}

	accounts, err := ks.ListAccounts(*walletName)
	if err != nil {
		fatal("list accounts: %v", err)
	}

	if len(accounts) == 0 {
		fmt.Println("No addresses found.")
		return
	}

	for _, acct := range accounts {
		fmt.Printf("  [%d] %s\n", acct.Index, acct.Address)
	}
}

func cmdWalletNewAddress(args []string, ksDir string) {
	fs := flag.NewFlagSet("wallet new-address", flag.ExitOnError)
	walletName := fs.String("wallet", "", "Wallet name")
	fs.Parse(args)

	if *walletName == "" {
		fatal("Usage: klingnet-cli wallet new-address --wallet <name>")
	}

	// Prompt for password.
	password, err := readPassword("Enter password: ")
	if err != nil {
		fatal("read password: %v", err)
	}

	// Load wallet.
	ks, err := wallet.NewKeystore(ksDir)
	if err != nil {
		fatal("open keystore: %v", err)
	}

	seed, err := ks.Load(*walletName, password)
	if err != nil {
		fatal("load wallet: %v", err)
	}

	// Derive next external address.
	master, err := wallet.NewMasterKey(seed)
	for i := range seed {
		seed[i] = 0
	}
	if err != nil {
		fatal("derive master key: %v", err)
	}

	nextIdx, err := ks.GetExternalIndex(*walletName)
	if err != nil {
		fatal("get external index: %v", err)
	}
	// Index 0 is the default account, new addresses start at 1.
	if nextIdx == 0 {
		nextIdx = 1
	}

	hdKey, err := master.DeriveAddress(0, wallet.ChangeExternal, nextIdx)
	if err != nil {
		fatal("derive address: %v", err)
	}
	addr := hdKey.Address()

	// Store account metadata.
	if err := ks.AddAccount(*walletName, wallet.AccountEntry{
		Index:   nextIdx,
		Name:    fmt.Sprintf("Address %d", nextIdx),
		Address: addr.String(),
	}); err != nil {
		fatal("add account: %v", err)
	}

	// Increment external index.
	if err := ks.IncrementExternalIndex(*walletName); err != nil {
		fatal("increment index: %v", err)
	}

	fmt.Printf("New address [%d]: %s\n", nextIdx, addr.String())
}

func cmdWalletConsolidate(args []string, rpcURL string) {
	fs := flag.NewFlagSet("wallet consolidate", flag.ExitOnError)
	walletName := fs.String("wallet", "", "Wallet name")
	chainID := fs.String("chain-id", "", "Optional sub-chain ID (32-byte hex)")
	maxInputs := fs.Uint("max-inputs", 500, "Max inputs to merge in one consolidation tx")
	fs.Parse(args)

	if *walletName == "" {
		fatal("Usage: klingnet-cli wallet consolidate --wallet <name> [--chain-id <hex>] [--max-inputs N]")
	}

	password, err := readPassword("Enter password: ")
	if err != nil {
		fatal("read password: %v", err)
	}

	client := rpcclient.New(rpcURL)
	var result rpc.WalletConsolidateResult
	if err := client.Call("wallet_consolidate", rpc.WalletConsolidateParam{
		Name:      *walletName,
		Password:  string(password),
		MaxInputs: uint32(*maxInputs),
		ChainID:   *chainID,
	}, &result); err != nil {
		fatal("wallet_consolidate: %v", err)
	}

	chainLabel := "root chain"
	if result.ChainID != "" {
		chainLabel = "sub-chain " + result.ChainID
	}
	fmt.Printf("Consolidation transaction submitted on %s\n", chainLabel)
	fmt.Printf("  Tx Hash:       %s\n", result.TxHash)
	fmt.Printf("  Inputs merged: %d\n", result.InputsUsed)
	fmt.Printf("  Input total:   %s KGX\n", formatAmount(result.InputTotal))
	fmt.Printf("  Fee:           %s KGX\n", formatAmount(result.Fee))
	fmt.Printf("  Output amount: %s KGX\n", formatAmount(result.OutputAmount))
	fmt.Println("\nRun this command again if you still have many small UTXOs.")
}

func cmdWalletBalance(args []string, ksDir, rpcURL string) {
	fs := flag.NewFlagSet("wallet balance", flag.ExitOnError)
	walletName := fs.String("wallet", "", "Wallet name (omit for all wallets)")
	fs.Parse(args)

	ks, err := wallet.NewKeystore(ksDir)
	if err != nil {
		fatal("open keystore: %v", err)
	}

	// If a name is given, show just that wallet; otherwise show all.
	var walletNames []string
	if *walletName != "" {
		walletNames = []string{*walletName}
	} else {
		walletNames, err = ks.List()
		if err != nil {
			fatal("list wallets: %v", err)
		}
	}

	if len(walletNames) == 0 {
		fmt.Println("No wallets found.")
		return
	}

	client := rpcclient.New(rpcURL)
	var grandTotal uint64
	var grandSpendable uint64

	for _, name := range walletNames {
		accounts, err := ks.ListAccounts(name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to read wallet %q: %v\n", name, err)
			continue
		}

		fmt.Printf("Wallet: %s\n", name)
		var walletTotal uint64
		var walletSpendable uint64
		var walletImmature uint64
		var walletStaked uint64
		var walletLocked uint64

		for _, acct := range accounts {
			var result rpc.BalanceResult
			if err := client.Call("utxo_getBalance", rpc.AddressParam{Address: acct.Address}, &result); err != nil {
				fmt.Fprintf(os.Stderr, "  Warning: failed to get balance for %s: %v\n", acct.Address, err)
				continue
			}

			fmt.Printf("  [%d] %s  spendable=%s KGX", acct.Index, acct.Address, formatAmount(result.Spendable))
			if result.Balance != result.Spendable {
				fmt.Printf(" (total=%s KGX", formatAmount(result.Balance))
				if result.Immature > 0 {
					fmt.Printf(", immature=%s", formatAmount(result.Immature))
				}
				if result.Staked > 0 {
					fmt.Printf(", staked=%s", formatAmount(result.Staked))
				}
				if result.Locked > 0 {
					fmt.Printf(", locked=%s", formatAmount(result.Locked))
				}
				fmt.Printf(")")
			}
			fmt.Println()

			walletTotal += result.Balance
			walletSpendable += result.Spendable
			walletImmature += result.Immature
			walletStaked += result.Staked
			walletLocked += result.Locked
		}

		fmt.Printf("  Spendable: %s KGX\n", formatAmount(walletSpendable))
		if walletTotal != walletSpendable {
			fmt.Printf("  Total: %s KGX", formatAmount(walletTotal))
			if walletImmature > 0 {
				fmt.Printf(" (immature=%s", formatAmount(walletImmature))
			}
			if walletStaked > 0 {
				if walletImmature > 0 {
					fmt.Printf(", staked=%s", formatAmount(walletStaked))
				} else {
					fmt.Printf(" (staked=%s", formatAmount(walletStaked))
				}
			}
			if walletLocked > 0 {
				if walletImmature > 0 || walletStaked > 0 {
					fmt.Printf(", locked=%s", formatAmount(walletLocked))
				} else {
					fmt.Printf(" (locked=%s", formatAmount(walletLocked))
				}
			}
			if walletImmature > 0 || walletStaked > 0 || walletLocked > 0 {
				fmt.Printf(")")
			}
			fmt.Println()
		}
		fmt.Println()
		grandTotal += walletTotal
		grandSpendable += walletSpendable
	}

	if len(walletNames) > 1 {
		fmt.Printf("Grand Spendable: %s KGX\n", formatAmount(grandSpendable))
		if grandTotal != grandSpendable {
			fmt.Printf("Grand Total: %s KGX\n", formatAmount(grandTotal))
		}
	}
}

func cmdWalletExportKey(args []string, ksDir string) {
	fs := flag.NewFlagSet("wallet export-key", flag.ExitOnError)
	walletName := fs.String("wallet", "", "Wallet name")
	output := fs.String("output", "", "Output file path (default: <name>.key)")
	account := fs.Uint("account", 0, "BIP-44 account index")
	index := fs.Uint("index", 0, "BIP-44 address index")
	fs.Parse(args)

	if *walletName == "" {
		fatal("Usage: klingnet-cli wallet export-key --wallet <name> [--output path] [--account 0] [--index 0]")
	}

	// Prompt for password.
	password, err := readPassword("Enter password: ")
	if err != nil {
		fatal("read password: %v", err)
	}

	// Load wallet.
	ks, err := wallet.NewKeystore(ksDir)
	if err != nil {
		fatal("open keystore: %v", err)
	}

	seed, err := ks.Load(*walletName, password)
	if err != nil {
		fatal("load wallet: %v", err)
	}

	// Derive key.
	master, err := wallet.NewMasterKey(seed)
	for i := range seed {
		seed[i] = 0
	}
	if err != nil {
		fatal("derive master key: %v", err)
	}

	hdKey, err := master.DeriveAddress(uint32(*account), wallet.ChangeExternal, uint32(*index))
	if err != nil {
		fatal("derive address key: %v", err)
	}

	privBytes := hdKey.PrivateKeyBytes()
	if privBytes == nil {
		fatal("no private key available")
	}

	pubBytes := hdKey.PublicKeyBytes()
	addr := hdKey.Address()

	privHex := hex.EncodeToString(privBytes)
	// Zero private key bytes.
	for i := range privBytes {
		privBytes[i] = 0
	}

	// Determine output path.
	outPath := *output
	if outPath == "" {
		outPath = *walletName + ".key"
	}

	// Write key file (0600).
	if err := os.WriteFile(outPath, []byte(privHex+"\n"), 0600); err != nil {
		fatal("write key file: %v", err)
	}

	fmt.Printf("Exported validator key to: %s\n", outPath)
	fmt.Printf("  Path:    m/44'/8888'/%d'/0/%d\n", *account, *index)
	fmt.Printf("  PubKey:  %s\n", hex.EncodeToString(pubBytes))
	fmt.Printf("  Address: %s\n", addr.String())
	fmt.Println("\nUse with: klingnetd --mine --validator-key", outPath)
}

func cmdWalletRescan(args []string, rpcURL string) {
	fs := flag.NewFlagSet("wallet rescan", flag.ExitOnError)
	walletName := fs.String("wallet", "", "Wallet name")
	fromHeight := fs.Uint64("from-height", 0, "Block height to start scanning from (default: 0)")
	deriveLimit := fs.Uint("derive-limit", 0, "Max addresses per chain to derive during rescan (default: auto)")
	timeoutSec := fs.Int("timeout", 600, "RPC timeout in seconds for this rescan request")
	chainID := fs.String("chain-id", "", "Sub-chain ID (hex) to scan instead of root chain")
	fs.Parse(args)

	if *walletName == "" {
		fatal("Usage: klingnet-cli wallet rescan --wallet <name> [--from-height N] [--derive-limit N] [--timeout sec] [--chain-id <hex>]")
	}

	password, err := readPassword("Enter password: ")
	if err != nil {
		fatal("read password: %v", err)
	}

	client := rpcclient.NewWithTimeout(rpcURL, time.Duration(*timeoutSec)*time.Second)
	var result rpc.WalletRescanResult
	if err := client.Call("wallet_rescan", rpc.WalletRescanParam{
		Name:        *walletName,
		Password:    string(password),
		FromHeight:  *fromHeight,
		DeriveLimit: uint32(*deriveLimit),
		ChainID:     *chainID,
	}, &result); err != nil {
		fatal("rescan: %v", err)
	}

	label := "root chain"
	if *chainID != "" {
		label = "sub-chain " + *chainID
	}
	fmt.Printf("Rescan complete on %s (blocks %d → %d)\n", label, result.FromHeight, result.ToHeight)
	fmt.Printf("  Addresses found: %d\n", result.AddressesFound)
	fmt.Printf("  New addresses:   %d\n", result.AddressesNew)
}

// ── Formatting helpers ─────────────────────────────────────────────────

// formatDifficulty returns a human-readable difficulty string (e.g. "1.05M").
func formatDifficulty(d uint64) string {
	switch {
	case d >= 1_000_000_000_000:
		return fmt.Sprintf("%.2fT", float64(d)/1_000_000_000_000)
	case d >= 1_000_000_000:
		return fmt.Sprintf("%.2fG", float64(d)/1_000_000_000)
	case d >= 1_000_000:
		return fmt.Sprintf("%.2fM", float64(d)/1_000_000)
	case d >= 1_000:
		return fmt.Sprintf("%.2fK", float64(d)/1_000)
	default:
		return fmt.Sprintf("%d", d)
	}
}

// formatAmount converts raw units to a human-readable decimal string.
func formatAmount(units uint64) string {
	whole := units / config.Coin
	frac := units % config.Coin
	return fmt.Sprintf("%d.%012d", whole, frac)
}

// parseAmount converts a decimal string to raw units.
func parseAmount(s string) (uint64, error) {
	if s == "" {
		return 0, fmt.Errorf("empty amount")
	}
	if strings.HasPrefix(s, "-") {
		return 0, fmt.Errorf("negative amount")
	}

	parts := strings.SplitN(s, ".", 2)

	whole, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid whole part: %w", err)
	}

	var frac uint64
	if len(parts) == 2 {
		fracStr := parts[1]
		if len(fracStr) > config.Decimals {
			return 0, fmt.Errorf("too many decimal places (max %d)", config.Decimals)
		}
		// Pad to Decimals digits.
		fracStr = fracStr + strings.Repeat("0", config.Decimals-len(fracStr))
		frac, err = strconv.ParseUint(fracStr, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid fractional part: %w", err)
		}
	}

	// Check overflow.
	if whole > math.MaxUint64/config.Coin {
		return 0, fmt.Errorf("amount too large")
	}
	result := whole * config.Coin
	if result > math.MaxUint64-frac {
		return 0, fmt.Errorf("amount too large")
	}

	return result + frac, nil
}

// ── mining ───────────────────────────────────────────────────────────────

func cmdMining(client *rpcclient.Client, args []string) {
	if len(args) < 1 {
		fatal("Usage: klingnet-cli mining <gettemplate|submit> [flags]")
	}

	switch args[0] {
	case "gettemplate":
		cmdMiningGetTemplate(client, args[1:])
	case "submit":
		cmdMiningSubmit(client, args[1:])
	default:
		fatal("Unknown mining command: %s\nUsage: klingnet-cli mining <gettemplate|submit> [flags]", args[0])
	}
}

func cmdMiningGetTemplate(client *rpcclient.Client, args []string) {
	fs := flag.NewFlagSet("mining gettemplate", flag.ExitOnError)
	chainIDStr := fs.String("chain", "", "Sub-chain ID (hex)")
	address := fs.String("address", "", "Coinbase address")
	fs.Parse(args)

	if *chainIDStr == "" || *address == "" {
		fatal("Usage: klingnet-cli mining gettemplate --chain <id> --address <coinbase>")
	}

	var result rpc.MiningBlockTemplateResult
	if err := client.Call("mining_getBlockTemplate", rpc.MiningGetBlockTemplateParam{
		ChainID:         *chainIDStr,
		CoinbaseAddress: *address,
	}, &result); err != nil {
		fatal("mining_getBlockTemplate: %v", err)
	}

	// Output as JSON for external miner consumption.
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fatal("marshal result: %v", err)
	}
	fmt.Println(string(data))
}

func cmdMiningSubmit(client *rpcclient.Client, args []string) {
	fs := flag.NewFlagSet("mining submit", flag.ExitOnError)
	chainIDStr := fs.String("chain", "", "Sub-chain ID (hex)")
	blockFile := fs.String("block", "", "Path to solved block JSON file")
	fs.Parse(args)

	if *chainIDStr == "" || *blockFile == "" {
		fatal("Usage: klingnet-cli mining submit --chain <id> --block <json_file>")
	}

	// Read block from file.
	blockData, err := os.ReadFile(*blockFile)
	if err != nil {
		fatal("read block file: %v", err)
	}

	var blk json.RawMessage
	if err := json.Unmarshal(blockData, &blk); err != nil {
		fatal("invalid block JSON: %v", err)
	}

	// Use raw params so the block JSON passes through without double-marshaling.
	params := map[string]interface{}{
		"chain_id": *chainIDStr,
		"block":    blk,
	}
	var result rpc.MiningSubmitBlockResult
	if err := client.Call("mining_submitBlock", params, &result); err != nil {
		fatal("mining_submitBlock: %v", err)
	}

	fmt.Printf("Block accepted!\n")
	fmt.Printf("  Hash:   %s\n", result.BlockHash)
	fmt.Printf("  Height: %d\n", result.Height)
}

// ── Password helper ─────────────────────────────────────────────────────

func readPassword(prompt string) ([]byte, error) {
	fmt.Fprint(os.Stderr, prompt)
	password, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Fprintln(os.Stderr) // newline after hidden input
	if err != nil {
		return nil, err
	}
	return password, nil
}

// ── Error helper ────────────────────────────────────────────────────────

func fatal(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "Error: "+format+"\n", args...)
	os.Exit(1)
}
