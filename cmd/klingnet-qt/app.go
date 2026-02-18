package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/Klingon-tech/klingnet-chain/config"
	"github.com/Klingon-tech/klingnet-chain/internal/node"
	"github.com/Klingon-tech/klingnet-chain/internal/rpcclient"
)

// qtSettings is the persistent configuration written to qt-settings.json.
type qtSettings struct {
	DataDir       string                   `json:"data_dir"`
	Network       string                   `json:"network"`
	ActiveWallet  string                   `json:"active_wallet"`
	Notifications bool                     `json:"notifications"`
	KnownAccounts map[string][]AccountInfo `json:"known_accounts,omitempty"`
}

// App manages application lifecycle and settings.
type App struct {
	ctx          context.Context
	rpcEndpoint  string
	dataDir      string
	networkName  string // "mainnet" or "testnet"
	activeWallet string // currently selected wallet name
	notify       bool

	// knownAccounts caches wallet addresses so balance works without unlock.
	mu            sync.RWMutex
	knownAccounts map[string][]AccountInfo

	// Embedded node.
	embeddedNode *node.Node
	startupErr   error // non-nil if the embedded node failed to start

	wallet   *WalletService
	chain    *ChainService
	network  *NetworkService
	staking  *StakingService
	subchain *SubChainService
}

// NewApp creates the application with default settings.
func NewApp() *App {
	app := &App{
		rpcEndpoint:   "http://127.0.0.1:8545",
		dataDir:       defaultDataDir(),
		networkName:   "mainnet",
		notify:        true,
		knownAccounts: make(map[string][]AccountInfo),
	}
	app.wallet = &WalletService{app: app}
	app.chain = &ChainService{app: app}
	app.network = &NetworkService{app: app}
	app.staking = &StakingService{app: app}
	app.subchain = &SubChainService{app: app}
	app.loadSettings()
	return app
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	// Load config from conf file (no CLI flags).
	network := config.NetworkType(a.networkName)
	cfg, err := config.LoadFromFile(a.dataDir, network)
	if err != nil {
		a.startupErr = fmt.Errorf("load config: %w", err)
		return
	}
	cfg.Wallet.Enabled = true // Always enable wallet in Qt.

	// Start embedded node.
	n, err := node.New(cfg)
	if err != nil {
		a.startupErr = fmt.Errorf("start node: %w", err)
		return
	}
	if err := n.Start(); err != nil {
		a.startupErr = fmt.Errorf("start services: %w", err)
		n.Stop()
		return
	}
	a.embeddedNode = n
	a.rpcEndpoint = "http://" + n.RPCAddr()
}

func (a *App) shutdown(_ context.Context) {
	if a.embeddedNode != nil {
		a.embeddedNode.Stop()
	}
}

// rpcClient returns a new RPC client for the configured endpoint.
func (a *App) rpcClient() *rpcclient.Client {
	return rpcclient.New(a.rpcEndpoint)
}

// keystorePath returns the keystore directory path.
// Matches klingnetd's layout: <dataDir>/<network>/keystore.
func (a *App) keystorePath() string {
	return filepath.Join(a.dataDir, a.networkName, "keystore")
}

// settingsPath returns the path to qt-settings.json.
func (a *App) settingsPath() string {
	return filepath.Join(a.dataDir, "qt-settings.json")
}

// ── Settings persistence ─────────────────────────────────────────────

func (a *App) loadSettings() {
	data, err := os.ReadFile(a.settingsPath())
	if err != nil {
		return // first launch or missing file — use defaults
	}
	var s qtSettings
	if err := json.Unmarshal(data, &s); err != nil {
		return
	}
	// Silently ignore old rpc_endpoint key from previous versions.
	if s.DataDir != "" {
		a.dataDir = s.DataDir
	}
	if s.Network != "" {
		a.networkName = s.Network
	}
	a.activeWallet = s.ActiveWallet
	a.notify = s.Notifications || !hasNotificationsKey(data)
	if s.KnownAccounts != nil {
		a.knownAccounts = s.KnownAccounts
	}
}

func (a *App) saveSettings() {
	a.mu.RLock()
	accts := make(map[string][]AccountInfo, len(a.knownAccounts))
	for k, v := range a.knownAccounts {
		accts[k] = v
	}
	a.mu.RUnlock()

	s := qtSettings{
		DataDir:       a.dataDir,
		Network:       a.networkName,
		ActiveWallet:  a.activeWallet,
		Notifications: a.notify,
		KnownAccounts: accts,
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	// Ensure directory exists.
	_ = os.MkdirAll(filepath.Dir(a.settingsPath()), 0700)
	_ = os.WriteFile(a.settingsPath(), data, 0600)
}

// ── Getters / Setters (each setter persists) ─────────────────────────

// GetDataDir returns the current data directory.
func (a *App) GetDataDir() string {
	return a.dataDir
}

// SetDataDir updates the data directory.
// Takes effect on next restart.
func (a *App) SetDataDir(dir string) {
	a.dataDir = dir
	a.saveSettings()
}

// GetNetwork returns the current network name ("mainnet" or "testnet").
func (a *App) GetNetwork() string {
	return a.networkName
}

// SetNetwork updates the network name.
// Takes effect on next restart.
func (a *App) SetNetwork(network string) {
	a.networkName = network
	a.saveSettings()
}

// GetActiveWallet returns the currently selected wallet name.
func (a *App) GetActiveWallet() string {
	return a.activeWallet
}

// SetActiveWallet updates the active wallet.
func (a *App) SetActiveWallet(name string) {
	a.activeWallet = name
	a.saveSettings()
}

// GetNotificationsEnabled returns whether desktop transaction notifications are enabled.
func (a *App) GetNotificationsEnabled() bool {
	return a.notify
}

// SetNotificationsEnabled enables/disables desktop transaction notifications.
func (a *App) SetNotificationsEnabled(enabled bool) {
	a.notify = enabled
	a.saveSettings()
}

// SendNotification sends an OS desktop notification.
// The browser Notification API is not available inside Wails' WebView,
// so the frontend calls this Go method instead.
// Platform-specific implementation is in notify_*.go files.
func (a *App) SendNotification(title, body string) {
	if !a.notify {
		return
	}
	sendOSNotification(title, body)
}

// ── Known accounts cache ─────────────────────────────────────────────

// SetKnownAccounts caches the account addresses for a wallet.
func (a *App) SetKnownAccounts(walletName string, accounts []AccountInfo) {
	a.mu.Lock()
	a.knownAccounts[walletName] = accounts
	a.mu.Unlock()
	a.saveSettings()
}

// GetKnownAccounts returns cached account addresses for a wallet.
// No password needed — these are just addresses, not keys.
func (a *App) GetKnownAccounts(walletName string) []AccountInfo {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.knownAccounts[walletName]
}

// TestConnection checks if the embedded node is reachable.
func (a *App) TestConnection() (bool, error) {
	var result struct {
		ChainID string `json:"chain_id"`
	}
	if err := a.rpcClient().Call("chain_getInfo", nil, &result); err != nil {
		return false, err
	}
	return true, nil
}

// GetStartupError returns the startup error message, or empty if OK.
func (a *App) GetStartupError() string {
	if a.startupErr != nil {
		return a.startupErr.Error()
	}
	return ""
}

// GetConfFilePath returns the path to the klingnet.conf file.
func (a *App) GetConfFilePath() string {
	cfg := config.Default(config.NetworkType(a.networkName))
	if a.dataDir != "" {
		cfg.DataDir = a.dataDir
	}
	return cfg.ConfigFile()
}

func defaultDataDir() string {
	return config.DefaultDataDir()
}

// hasNotificationsKey detects if "notifications" exists in settings JSON.
func hasNotificationsKey(data []byte) bool {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return false
	}
	_, ok := raw["notifications"]
	return ok
}
