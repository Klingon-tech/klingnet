// Package rpc implements the JSON-RPC 2.0 API server.
package rpc

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/Klingon-tech/klingnet-chain/config"
	"github.com/Klingon-tech/klingnet-chain/internal/chain"
	"github.com/Klingon-tech/klingnet-chain/internal/consensus"
	klog "github.com/Klingon-tech/klingnet-chain/internal/log"
	"github.com/Klingon-tech/klingnet-chain/internal/mempool"
	"github.com/Klingon-tech/klingnet-chain/internal/p2p"
	"github.com/Klingon-tech/klingnet-chain/internal/subchain"
	"github.com/Klingon-tech/klingnet-chain/internal/token"
	"github.com/Klingon-tech/klingnet-chain/internal/utxo"
	"github.com/Klingon-tech/klingnet-chain/internal/wallet"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
	"github.com/rs/zerolog"
)

// maxBodySize is the maximum allowed request body size (1 MB).
const maxBodySize = 1 << 20

// Server is the JSON-RPC 2.0 HTTP server.
type Server struct {
	addr        string
	chain       *chain.Chain
	utxos       *utxo.Store
	pool        *mempool.Pool
	p2pNode     *p2p.Node
	genesis     *config.Genesis
	engine      consensus.Engine                       // For validator queries.
	scManager   *subchain.Manager                      // For sub-chain queries (nil = disabled).
	keystore    *wallet.Keystore                       // For wallet RPC (nil = disabled).
	tokenStore  *token.Store                           // For token queries (nil = disabled).
	tracker     *consensus.ValidatorTracker            // For root chain validator status (nil = disabled).
	scTrackers  map[string]*consensus.ValidatorTracker // chainID hex → sub-chain tracker
	txIndex     *WalletTxIndex                         // For indexed wallet history (nil = scan fallback).
	banManager  *p2p.BanManager                        // For net_getBanList (nil = disabled).
	server      *http.Server
	logger      zerolog.Logger
	ln          net.Listener
	allowedNets []*net.IPNet // Empty = allow all.
	corsOrigins []string     // Empty = no CORS headers.
}

// New creates a new RPC server. The rpcCfg parameter controls IP filtering
// and CORS. A zero-value RPCConfig allows all IPs and disables CORS.
// The engine parameter is optional (nil disables stake_* endpoints).
func New(addr string, ch *chain.Chain, utxos *utxo.Store, pool *mempool.Pool,
	p2pNode *p2p.Node, genesis *config.Genesis, engine consensus.Engine, rpcCfg ...config.RPCConfig) *Server {

	s := &Server{
		addr:       addr,
		chain:      ch,
		utxos:      utxos,
		pool:       pool,
		p2pNode:    p2pNode,
		genesis:    genesis,
		engine:     engine,
		scTrackers: make(map[string]*consensus.ValidatorTracker),
		logger:     klog.WithComponent("rpc"),
	}

	if len(rpcCfg) > 0 {
		s.allowedNets = parseAllowedIPs(rpcCfg[0].AllowedIPs)
		s.corsOrigins = rpcCfg[0].CORSOrigins
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleRequest)

	s.server = &http.Server{
		Handler:     mux,
		ReadTimeout: 30 * time.Second,
		// Some wallet operations (e.g. deep rescan) are intentionally long-running.
		WriteTimeout: 10 * time.Minute,
	}

	return s
}

// parseAllowedIPs converts string IP/CIDR entries into net.IPNet.
func parseAllowedIPs(entries []string) []*net.IPNet {
	var nets []*net.IPNet
	for _, entry := range entries {
		_, ipNet, err := net.ParseCIDR(entry)
		if err == nil {
			nets = append(nets, ipNet)
			continue
		}
		// Try as a single IP (add /32 or /128).
		ip := net.ParseIP(entry)
		if ip == nil {
			continue
		}
		bits := 32
		if ip.To4() == nil {
			bits = 128
		}
		nets = append(nets, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
	}
	return nets
}

// Start begins listening and serving in a background goroutine.
// It returns immediately after the listener is bound.
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("rpc listen: %w", err)
	}
	s.ln = ln

	go func() {
		if err := s.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			s.logger.Error().Err(err).Msg("RPC server error")
		}
	}()

	return nil
}

// Addr returns the listener address (useful when bound to :0).
func (s *Server) Addr() string {
	if s.ln != nil {
		return s.ln.Addr().String()
	}
	return s.addr
}

// Stop gracefully shuts down the server.
func (s *Server) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.server.Shutdown(ctx)
}

// SetSubChainManager sets the sub-chain manager for sub-chain RPC endpoints.
func (s *Server) SetSubChainManager(mgr *subchain.Manager) {
	s.scManager = mgr
}

// SetKeystore sets the wallet keystore for wallet RPC endpoints.
func (s *Server) SetKeystore(ks *wallet.Keystore) {
	s.keystore = ks
}

// SetTokenStore sets the token metadata store for token RPC endpoints.
func (s *Server) SetTokenStore(ts *token.Store) {
	s.tokenStore = ts
}

// SetValidatorTracker sets the root chain validator liveness tracker.
func (s *Server) SetValidatorTracker(t *consensus.ValidatorTracker) {
	s.tracker = t
}

// SetSubChainTracker sets a sub-chain's validator liveness tracker.
func (s *Server) SetSubChainTracker(chainIDHex string, t *consensus.ValidatorTracker) {
	s.scTrackers[chainIDHex] = t
}

// RemoveSubChainTracker removes a sub-chain's validator tracker.
func (s *Server) RemoveSubChainTracker(chainIDHex string) {
	delete(s.scTrackers, chainIDHex)
}

// SetBanManager sets the ban manager for net_getBanList.
func (s *Server) SetBanManager(bm *p2p.BanManager) {
	s.banManager = bm
}

// SetWalletTxIndex sets the persistent wallet transaction index.
func (s *Server) SetWalletTxIndex(idx *WalletTxIndex) {
	s.txIndex = idx
}

// handleRequest is the main HTTP handler for JSON-RPC requests.
func (s *Server) handleRequest(w http.ResponseWriter, r *http.Request) {
	// IP filtering.
	if len(s.allowedNets) > 0 {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		ip := net.ParseIP(host)
		if ip == nil || !s.isIPAllowed(ip) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}

	// CORS headers.
	s.setCORSHeaders(w, r)

	// Handle CORS preflight.
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if r.Method != http.MethodPost {
		writeError(w, nil, CodeInvalidRequest, "only POST method is allowed")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodySize+1))
	if err != nil {
		writeError(w, nil, CodeParseError, "failed to read request body")
		return
	}
	if len(body) > maxBodySize {
		writeError(w, nil, CodeInvalidRequest, "request body too large")
		return
	}

	var req Request
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, nil, CodeParseError, "invalid JSON")
		return
	}

	if req.JSONRPC != "2.0" {
		writeError(w, req.ID, CodeInvalidRequest, "jsonrpc must be \"2.0\"")
		return
	}

	result, rpcErr := s.dispatch(&req)
	if rpcErr != nil {
		writeJSON(w, Response{
			JSONRPC: "2.0",
			Error:   rpcErr,
			ID:      req.ID,
		})
		return
	}

	writeJSON(w, Response{
		JSONRPC: "2.0",
		Result:  result,
		ID:      req.ID,
	})
}

// dispatch routes a request to the appropriate handler.
func (s *Server) dispatch(req *Request) (interface{}, *Error) {
	switch req.Method {
	case "chain_getInfo":
		return s.handleChainGetInfo(req)
	case "chain_getBlockByHash":
		return s.handleChainGetBlockByHash(req)
	case "chain_getBlockByHeight":
		return s.handleChainGetBlockByHeight(req)
	case "chain_getTransaction":
		return s.handleChainGetTransaction(req)
	case "utxo_get":
		return s.handleUTXOGet(req)
	case "utxo_getByAddress":
		return s.handleUTXOGetByAddress(req)
	case "utxo_getBalance":
		return s.handleUTXOGetBalance(req)
	case "tx_submit":
		return s.handleTxSubmit(req)
	case "tx_validate":
		return s.handleTxValidate(req)
	case "mempool_getInfo":
		return s.handleMempoolGetInfo(req)
	case "mempool_getContent":
		return s.handleMempoolGetContent(req)
	case "net_getPeerInfo":
		return s.handleNetGetPeerInfo(req)
	case "net_getNodeInfo":
		return s.handleNetGetNodeInfo(req)
	case "net_getBanList":
		return s.handleNetGetBanList(req)
	case "stake_getInfo":
		return s.handleStakeGetInfo(req)
	case "stake_getValidators":
		return s.handleStakeGetValidators(req)
	case "subchain_list":
		return s.handleSubChainList(req)
	case "subchain_getInfo":
		return s.handleSubChainGetInfo(req)
	case "subchain_getBalance":
		return s.handleSubChainGetBalance(req)
	case "subchain_send":
		return s.handleSubChainSend(req)
	case "subchain_stake":
		return s.handleSubChainStake(req)
	case "subchain_unstake":
		return s.handleSubChainUnstake(req)
	case "wallet_create":
		return s.handleWalletCreate(req)
	case "wallet_import":
		return s.handleWalletImport(req)
	case "wallet_list":
		return s.handleWalletList(req)
	case "wallet_newAddress":
		return s.handleWalletNewAddress(req)
	case "wallet_listAddresses":
		return s.handleWalletListAddresses(req)
	case "wallet_send":
		return s.handleWalletSend(req)
	case "wallet_consolidate":
		return s.handleWalletConsolidate(req)
	case "wallet_sendMany":
		return s.handleWalletSendMany(req)
	case "wallet_exportKey":
		return s.handleWalletExportKey(req)
	case "wallet_stake":
		return s.handleWalletStake(req)
	case "wallet_mintToken":
		return s.handleWalletMintToken(req)
	case "wallet_unstake":
		return s.handleWalletUnstake(req)
	case "wallet_sendToken":
		return s.handleWalletSendToken(req)
	case "wallet_createSubChain":
		return s.handleWalletCreateSubChain(req)
	case "wallet_getHistory":
		return s.handleWalletGetHistory(req)
	case "wallet_rescan":
		return s.handleWalletRescan(req)
	case "token_getInfo":
		return s.handleTokenGetInfo(req)
	case "token_getBalance":
		return s.handleTokenGetBalance(req)
	case "token_list":
		return s.handleTokenList(req)
	case "mining_getBlockTemplate":
		return s.handleMiningGetBlockTemplate(req)
	case "mining_submitBlock":
		return s.handleMiningSubmitBlock(req)
	case "validator_getStatus":
		return s.handleValidatorGetStatus(req)
	default:
		return nil, &Error{Code: CodeMethodNotFound, Message: fmt.Sprintf("method %q not found", req.Method)}
	}
}

// writeJSON writes a JSON-RPC response.
func writeJSON(w http.ResponseWriter, resp Response) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// writeError writes a JSON-RPC error response.
func writeError(w http.ResponseWriter, id interface{}, code int, message string) {
	writeJSON(w, Response{
		JSONRPC: "2.0",
		Error:   &Error{Code: code, Message: message},
		ID:      id,
	})
}

// isIPAllowed checks if the IP is in the allowed networks list.
func (s *Server) isIPAllowed(ip net.IP) bool {
	for _, n := range s.allowedNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// setCORSHeaders adds CORS headers based on the configured origins.
func (s *Server) setCORSHeaders(w http.ResponseWriter, r *http.Request) {
	if len(s.corsOrigins) == 0 {
		return
	}

	origin := r.Header.Get("Origin")
	if origin == "" {
		return
	}

	// Check if origin is allowed.
	allowed := false
	for _, o := range s.corsOrigins {
		if o == "*" {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			allowed = true
			break
		}
		if o == origin {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			allowed = true
			break
		}
	}

	if allowed {
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	}
}

// parseParams unmarshals the request params into the given target.
func parseParams(req *Request, target interface{}) *Error {
	if req.Params == nil {
		return &Error{Code: CodeInvalidParams, Message: "params required"}
	}

	data, err := json.Marshal(req.Params)
	if err != nil {
		return &Error{Code: CodeInvalidParams, Message: "invalid params"}
	}

	if err := json.Unmarshal(data, target); err != nil {
		return &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("invalid params: %v", err)}
	}
	return nil
}

// chainContext holds chain/utxo/pool/genesis for either root or a sub-chain.
type chainContext struct {
	chain   *chain.Chain
	utxos   *utxo.Store
	pool    *mempool.Pool
	genesis *config.Genesis
}

// extractChainID pulls an optional chain_id string from raw request params.
// Returns "" if params is nil or chain_id is absent.
func extractChainID(req *Request) string {
	if req.Params == nil {
		return ""
	}
	data, err := json.Marshal(req.Params)
	if err != nil {
		return ""
	}
	var field struct {
		ChainID string `json:"chain_id"`
	}
	if err := json.Unmarshal(data, &field); err != nil {
		return ""
	}
	return field.ChainID
}

// resolveChain returns root or sub-chain context based on chainIDHex.
// An empty string returns the root chain context.
func (s *Server) resolveChain(chainIDHex string) (*chainContext, *Error) {
	if chainIDHex == "" {
		return &chainContext{
			chain:   s.chain,
			utxos:   s.utxos,
			pool:    s.pool,
			genesis: s.genesis,
		}, nil
	}

	if s.scManager == nil {
		return nil, &Error{Code: CodeNotFound, Message: "sub-chains not enabled"}
	}

	chainIDBytes, err := hex.DecodeString(chainIDHex)
	if err != nil || len(chainIDBytes) != types.HashSize {
		return nil, &Error{Code: CodeInvalidParams, Message: "invalid chain_id: must be 32-byte hex"}
	}

	var chainID types.ChainID
	copy(chainID[:], chainIDBytes)

	sr, ok := s.scManager.GetChain(chainID)
	if !ok {
		return nil, &Error{Code: CodeNotFound, Message: fmt.Sprintf("sub-chain %s not synced on this node", chainIDHex)}
	}

	return &chainContext{
		chain:   sr.Chain,
		utxos:   sr.UTXOs,
		pool:    sr.Pool,
		genesis: sr.Genesis,
	}, nil
}
