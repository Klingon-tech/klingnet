// Package p2p implements peer-to-peer networking using libp2p.
package p2p

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Klingon-tech/klingnet-chain/config"
	klog "github.com/Klingon-tech/klingnet-chain/internal/log"
	"github.com/Klingon-tech/klingnet-chain/internal/storage"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	drouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	dutil "github.com/libp2p/go-libp2p/p2p/discovery/util"
)

const (
	// dhtRendezvousFallback is the default DHT namespace when no NetworkID is set.
	dhtRendezvousFallback = "klingnet-chain"

	// dhtDiscoveryInterval is how often DHT FindPeers runs.
	dhtDiscoveryInterval = 30 * time.Second

	// peerConnectTimeout is the timeout for connecting to a persisted peer.
	peerConnectTimeout = 5 * time.Second
)

// Config holds P2P node configuration.
type Config struct {
	ListenAddr string
	Port       int
	Seeds      []string
	MaxPeers   int
	NoDiscover bool
	DB         storage.DB // Peer persistence (nil = disabled, for tests)
	DHTServer  bool       // Run DHT in server mode (for seeds/validators)
	NetworkID  string     // e.g. "klingnet-mainnet-1" — isolates DHT per network
	DataDir    string     // Data directory for persisting node identity
}

// Node represents a P2P node built on libp2p.
type Node struct {
	host   host.Host
	pubsub *pubsub.PubSub
	config Config
	ctx    context.Context
	cancel context.CancelFunc

	topicTx    *pubsub.Topic
	topicBlock *pubsub.Topic
	subTx      *pubsub.Subscription
	subBlock   *pubsub.Subscription

	txHandler    func(peer.ID, []byte)
	blockHandler func(peer.ID, []byte)

	// Heartbeat topic for validator liveness.
	topicHeartbeat   *pubsub.Topic
	subHeartbeat     *pubsub.Subscription
	heartbeatHandler func(*HeartbeatMessage)

	// Sub-chain per-chain GossipSub topics.
	scMu            sync.RWMutex
	scTopics        map[string]*pubsub.Topic           // chainID hex → block topic
	scSubs          map[string]*pubsub.Subscription    // chainID hex → block subscription
	scTxTopics      map[string]*pubsub.Topic           // chainID hex → tx topic
	scTxSubs        map[string]*pubsub.Subscription    // chainID hex → tx subscription
	scBlockHandlers map[string]func(peer.ID, []byte)    // chainID hex → block handler
	scTxHandlers    map[string]func(peer.ID, []byte)    // chainID hex → tx handler
	scHBTopics      map[string]*pubsub.Topic           // chainID hex → heartbeat topic
	scHBSubs        map[string]*pubsub.Subscription    // chainID hex → heartbeat subscription
	scHBHandlers    map[string]func(*HeartbeatMessage) // chainID hex → heartbeat handler

	mu    sync.RWMutex
	peers map[peer.ID]*Peer

	BanManager      *BanManager   // nil if Config.DB is nil
	peerStore       *PeerStore    // nil if Config.DB is nil
	dht             *dht.IpfsDHT  // nil if NoDiscover
	connNotify      *connNotifier // connection lifecycle tracker
	onPeerConnected func()        // optional callback when a peer connects

	// Handshake fields.
	genesisHash      types.Hash
	handshakeEnabled bool
	heightFn         func() uint64
}

// New creates a new P2P node with the given config.
func New(cfg Config) *Node {
	ctx, cancel := context.WithCancel(context.Background())
	n := &Node{
		config:          cfg,
		ctx:             ctx,
		cancel:          cancel,
		peers:           make(map[peer.ID]*Peer),
		scTopics:        make(map[string]*pubsub.Topic),
		scSubs:          make(map[string]*pubsub.Subscription),
		scTxTopics:      make(map[string]*pubsub.Topic),
		scTxSubs:        make(map[string]*pubsub.Subscription),
		scBlockHandlers: make(map[string]func(peer.ID, []byte)),
		scTxHandlers:    make(map[string]func(peer.ID, []byte)),
		scHBTopics:      make(map[string]*pubsub.Topic),
		scHBSubs:        make(map[string]*pubsub.Subscription),
		scHBHandlers:    make(map[string]func(*HeartbeatMessage)),
	}
	if cfg.DB != nil {
		n.peerStore = NewPeerStore(cfg.DB)
	}
	return n
}

// rendezvous returns the DHT/mDNS discovery namespace for this node.
// When NetworkID is set, it isolates peer discovery per network.
func (n *Node) rendezvous() string {
	if n.config.NetworkID != "" {
		return "klingnet/" + n.config.NetworkID
	}
	return dhtRendezvousFallback
}

// Start initializes the libp2p host, pubsub, and begins listening.
func (n *Node) Start() error {
	addr := fmt.Sprintf("/ip4/%s/tcp/%d", n.config.ListenAddr, n.config.Port)

	// Create ban manager (before host, so the gater can reference it).
	if n.config.DB != nil {
		banStore := NewBanStore(n.config.DB)
		n.BanManager = NewBanManager(banStore, n)
		n.BanManager.LoadBans()
	} else {
		n.BanManager = NewBanManager(nil, n)
	}

	opts := []libp2p.Option{
		libp2p.ListenAddrStrings(addr),
		libp2p.ConnectionGater(&banGater{banMgr: n.BanManager}),
	}

	// Load or generate persistent identity so peer ID survives restarts.
	if n.config.DataDir != "" {
		privKey, err := loadOrCreateIdentity(n.config.DataDir)
		if err != nil {
			return fmt.Errorf("load p2p identity: %w", err)
		}
		opts = append(opts, libp2p.Identity(privKey))
	}

	h, err := libp2p.New(opts...)
	if err != nil {
		return fmt.Errorf("create libp2p host: %w", err)
	}
	n.host = h

	// Register connection notifier for peer tracking.
	n.connNotify = &connNotifier{node: n}
	h.Network().Notify(n.connNotify)

	// Init DHT before GossipSub so the DHT can serve as a peer source.
	if !n.config.NoDiscover {
		if err := n.initDHT(); err != nil {
			h.Close()
			return fmt.Errorf("init dht: %w", err)
		}
	}

	// Set up GossipSub for message propagation.
	ps, err := pubsub.NewGossipSub(n.ctx, h,
		pubsub.WithMaxMessageSize(config.MaxBlockSize+64*1024),
	)
	if err != nil {
		n.closeDHT()
		h.Close()
		return fmt.Errorf("create pubsub: %w", err)
	}
	n.pubsub = ps

	// Join topics.
	if err := n.joinTopics(); err != nil {
		n.closeDHT()
		h.Close()
		return err
	}

	// Register handshake stream handler if enabled.
	if n.handshakeEnabled {
		n.registerHandshakeHandler()
	}

	// Start message loops.
	go n.readLoop(n.subTx, n.handleTxMessage)
	go n.readLoop(n.subBlock, n.handleBlockMessage)

	// Load and reconnect persisted peers in background.
	go n.loadPersistedPeers()

	// Connect to seed peers (first attempt is blocking, retries run in background).
	if len(n.config.Seeds) > 0 {
		l := klog.WithComponent("p2p")
		l.Info().Int("seeds", len(n.config.Seeds)).Msg("Connecting to seeds...")
	}
	n.connectSeedsOnce()
	go n.connectSeedsLoop()

	// Start peer discovery.
	if !n.config.NoDiscover {
		n.startMDNS()
		go n.runDHTDiscovery()
	}

	// Start peer persistence loop.
	if n.peerStore != nil {
		go n.runPersistLoop()
	}

	return nil
}

// Stop shuts down the P2P node.
func (n *Node) Stop() error {
	// Persist peers one final time before shutdown.
	n.persistPeers()

	n.cancel()
	if n.subTx != nil {
		n.subTx.Cancel()
	}
	if n.subBlock != nil {
		n.subBlock.Cancel()
	}

	// Cancel heartbeat subscription.
	if n.subHeartbeat != nil {
		n.subHeartbeat.Cancel()
	}
	if n.topicHeartbeat != nil {
		n.topicHeartbeat.Close()
	}

	// Cancel all sub-chain subscriptions.
	n.scMu.Lock()
	for id, sub := range n.scSubs {
		sub.Cancel()
		delete(n.scSubs, id)
	}
	for id, sub := range n.scTxSubs {
		sub.Cancel()
		delete(n.scTxSubs, id)
	}
	for id, t := range n.scTopics {
		t.Close()
		delete(n.scTopics, id)
	}
	for id, t := range n.scTxTopics {
		t.Close()
		delete(n.scTxTopics, id)
	}
	for id, sub := range n.scHBSubs {
		sub.Cancel()
		delete(n.scHBSubs, id)
	}
	for id, t := range n.scHBTopics {
		t.Close()
		delete(n.scHBTopics, id)
	}
	n.scMu.Unlock()

	n.closeDHT()
	if n.host != nil {
		return n.host.Close()
	}
	return nil
}

// Host returns the underlying libp2p host (nil before Start).
func (n *Node) Host() host.Host {
	return n.host
}

// SetPeerConnectedHandler registers a callback invoked when a new peer connects.
func (n *Node) SetPeerConnectedHandler(fn func()) {
	n.onPeerConnected = fn
}

// SetGenesisHash sets the genesis hash for handshake validation.
// A non-zero hash enables the handshake protocol.
func (n *Node) SetGenesisHash(h types.Hash) {
	n.genesisHash = h
	n.handshakeEnabled = h != (types.Hash{})
}

// SetHeightFn sets the function used to report best height during handshake.
func (n *Node) SetHeightFn(fn func() uint64) {
	n.heightFn = fn
}

// DisconnectPeer closes all connections to a peer and removes it from the peer list.
func (n *Node) DisconnectPeer(id peer.ID) error {
	if n.host == nil {
		return fmt.Errorf("node not started")
	}
	n.removePeer(id)
	return n.host.Network().ClosePeer(id)
}

// ID returns the peer ID of this node.
func (n *Node) ID() peer.ID {
	if n.host == nil {
		return ""
	}
	return n.host.ID()
}

// Addrs returns the full multiaddrs of this node.
func (n *Node) Addrs() []string {
	if n.host == nil {
		return nil
	}
	var addrs []string
	for _, a := range n.host.Addrs() {
		addrs = append(addrs, fmt.Sprintf("%s/p2p/%s", a, n.host.ID()))
	}
	return addrs
}

// SetTxHandler registers a callback for incoming transactions.
// The callback receives the sender peer ID and the raw message bytes.
func (n *Node) SetTxHandler(fn func(from peer.ID, data []byte)) {
	n.txHandler = fn
}

// SetBlockHandler registers a callback for incoming blocks.
// The callback receives the sender peer ID and the raw message bytes.
func (n *Node) SetBlockHandler(fn func(from peer.ID, data []byte)) {
	n.blockHandler = fn
}

// PeerCount returns the number of connected peers.
func (n *Node) PeerCount() int {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return len(n.peers)
}

// PeerList returns a snapshot of connected peers.
func (n *Node) PeerList() []*Peer {
	n.mu.RLock()
	defer n.mu.RUnlock()
	out := make([]*Peer, 0, len(n.peers))
	for _, p := range n.peers {
		out = append(out, p)
	}
	return out
}

func (n *Node) addPeer(id peer.ID) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if _, exists := n.peers[id]; !exists {
		n.peers[id] = &Peer{
			ID:          id,
			ConnectedAt: time.Now(),
		}
	}
}

func (n *Node) removePeer(id peer.ID) {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.peers, id)
}

func (n *Node) joinTopics() error {
	var err error
	n.topicTx, err = n.pubsub.Join(TopicTransactions)
	if err != nil {
		return fmt.Errorf("join tx topic: %w", err)
	}
	n.topicBlock, err = n.pubsub.Join(TopicBlocks)
	if err != nil {
		return fmt.Errorf("join block topic: %w", err)
	}
	n.subTx, err = n.topicTx.Subscribe()
	if err != nil {
		return fmt.Errorf("subscribe tx: %w", err)
	}
	n.subBlock, err = n.topicBlock.Subscribe()
	if err != nil {
		return fmt.Errorf("subscribe block: %w", err)
	}
	return nil
}

func (n *Node) readLoop(sub *pubsub.Subscription, handler func(*pubsub.Message)) {
	for {
		msg, err := sub.Next(n.ctx)
		if err != nil {
			return // Context cancelled.
		}
		if msg.ReceivedFrom == n.host.ID() {
			continue // Skip own messages.
		}
		handler(msg)
	}
}

func (n *Node) handleTxMessage(msg *pubsub.Message) {
	defer func() { recover() }()
	n.addPeer(msg.ReceivedFrom)
	if n.txHandler != nil {
		n.txHandler(msg.ReceivedFrom, msg.Data)
	}
}

func (n *Node) handleBlockMessage(msg *pubsub.Message) {
	defer func() { recover() }()
	n.addPeer(msg.ReceivedFrom)
	if n.blockHandler != nil {
		n.blockHandler(msg.ReceivedFrom, msg.Data)
	}
}

func (n *Node) startMDNS() {
	svc := mdns.NewMdnsService(n.host, n.rendezvous(), &discoveryNotifee{node: n})
	// mDNS failure is non-fatal.
	_ = svc.Start()
}

// connectSeedsOnce tries to connect to each seed peer once (blocking).
// Returns true if at least one seed connected.
func (n *Node) connectSeedsOnce() bool {
	logger := klog.WithComponent("p2p")
	connected := false
	for _, addr := range n.config.Seeds {
		info, err := peer.AddrInfoFromString(addr)
		if err != nil {
			logger.Warn().Str("addr", addr).Err(err).Msg("Bad seed address")
			continue
		}
		ctx, cancel := context.WithTimeout(n.ctx, 10*time.Second)
		err = n.host.Connect(ctx, *info)
		cancel()
		if err != nil {
			logger.Warn().Str("peer", info.ID.String()[:16]).Err(err).Msg("Seed connect failed")
		} else {
			n.addPeer(info.ID)
			logger.Info().Str("peer", info.ID.String()[:16]).Msg("Seed connected")
			connected = true
		}
	}
	return connected
}

// connectSeedsLoop retries seed connections every 10s forever.
func (n *Node) connectSeedsLoop() {
	if len(n.config.Seeds) == 0 {
		return
	}
	logger := klog.WithComponent("p2p")

	for {
		select {
		case <-n.ctx.Done():
			return
		case <-time.After(10 * time.Second):
			if n.PeerCount() == 0 {
				logger.Info().Int("seeds", len(n.config.Seeds)).Msg("No peers, retrying seeds...")
				n.connectSeedsOnce()
			}
		}
	}
}

// --- DHT ---

func (n *Node) initDHT() error {
	mode := dht.ModeClient
	if n.config.DHTServer {
		mode = dht.ModeServer
	}
	kadDHT, err := dht.New(n.ctx, n.host, dht.Mode(mode))
	if err != nil {
		return fmt.Errorf("create kad-dht: %w", err)
	}
	n.dht = kadDHT
	return kadDHT.Bootstrap(n.ctx)
}

func (n *Node) closeDHT() {
	if n.dht != nil {
		n.dht.Close()
		n.dht = nil
	}
}

func (n *Node) runDHTDiscovery() {
	if n.dht == nil {
		return
	}

	routingDiscovery := drouting.NewRoutingDiscovery(n.dht)
	dutil.Advertise(n.ctx, routingDiscovery, n.rendezvous())

	ticker := time.NewTicker(dhtDiscoveryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-n.ctx.Done():
			return
		case <-ticker.C:
			n.findDHTPeers(routingDiscovery)
		}
	}
}

func (n *Node) findDHTPeers(routingDiscovery *drouting.RoutingDiscovery) {
	ctx, cancel := context.WithTimeout(n.ctx, 20*time.Second)
	defer cancel()

	peerCh, err := routingDiscovery.FindPeers(ctx, n.rendezvous())
	if err != nil {
		return
	}

	for p := range peerCh {
		if p.ID == n.host.ID() || len(p.Addrs) == 0 {
			continue
		}

		// Respect MaxPeers.
		if n.config.MaxPeers > 0 && n.PeerCount() >= n.config.MaxPeers {
			return
		}

		connectCtx, connectCancel := context.WithTimeout(n.ctx, peerConnectTimeout)
		if err := n.host.Connect(connectCtx, p); err == nil {
			n.mu.Lock()
			if existing, ok := n.peers[p.ID]; ok && existing.Source == "" {
				existing.Source = "dht"
			}
			n.mu.Unlock()
		}
		connectCancel()
	}
}

// --- Peer Persistence ---

func (n *Node) persistPeers() {
	if n.peerStore == nil || n.host == nil {
		return
	}

	n.mu.RLock()
	snapshot := make([]peer.ID, 0, len(n.peers))
	sources := make(map[peer.ID]string)
	for id, p := range n.peers {
		snapshot = append(snapshot, id)
		sources[id] = p.Source
	}
	n.mu.RUnlock()

	now := time.Now().Unix()
	for _, id := range snapshot {
		addrs := n.host.Peerstore().Addrs(id)
		addrStrs := make([]string, len(addrs))
		for i, a := range addrs {
			addrStrs[i] = a.String()
		}
		rec := PeerRecord{
			ID:       id.String(),
			Addrs:    addrStrs,
			LastSeen: now,
			Source:   sources[id],
		}
		n.peerStore.Save(rec) // Best-effort, ignore errors.
	}
}

func (n *Node) loadPersistedPeers() {
	if n.peerStore == nil {
		return
	}

	// Prune stale records first.
	n.peerStore.PruneStale(staleThreshold)

	records, err := n.peerStore.LoadAll()
	if err != nil {
		return
	}

	for _, rec := range records {
		id, err := peer.Decode(rec.ID)
		if err != nil {
			continue
		}
		if id == n.host.ID() {
			continue
		}

		// Build AddrInfo from stored addresses.
		info := peer.AddrInfo{ID: id}
		for _, addr := range rec.Addrs {
			ma, err := peer.AddrInfoFromString(fmt.Sprintf("%s/p2p/%s", addr, rec.ID))
			if err != nil {
				continue
			}
			info.Addrs = append(info.Addrs, ma.Addrs...)
		}

		if len(info.Addrs) == 0 {
			continue
		}

		ctx, cancel := context.WithTimeout(n.ctx, peerConnectTimeout)
		n.host.Connect(ctx, info) // Best-effort reconnect.
		cancel()
	}
}

func (n *Node) runPersistLoop() {
	ticker := time.NewTicker(persistInterval)
	defer ticker.Stop()

	for {
		select {
		case <-n.ctx.Done():
			return
		case <-ticker.C:
			n.persistPeers()
			n.peerStore.PruneStale(staleThreshold)
		}
	}
}

// loadOrCreateIdentity loads a persisted libp2p identity key from dataDir,
// or generates a new one and saves it. This ensures the peer ID is stable.
func loadOrCreateIdentity(dataDir string) (libp2pcrypto.PrivKey, error) {
	keyPath := filepath.Join(dataDir, "node.key")

	// Try loading existing key.
	data, err := os.ReadFile(keyPath)
	if err == nil {
		keyBytes, err := hex.DecodeString(string(data))
		if err != nil {
			return nil, fmt.Errorf("decode node key: %w", err)
		}
		return libp2pcrypto.UnmarshalEd25519PrivateKey(keyBytes)
	}

	// Generate new Ed25519 key.
	priv, _, err := libp2pcrypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}

	// Save raw bytes as hex.
	raw, err := priv.Raw()
	if err != nil {
		return nil, fmt.Errorf("marshal key: %w", err)
	}
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	if err := os.WriteFile(keyPath, []byte(hex.EncodeToString(raw)), 0600); err != nil {
		return nil, fmt.Errorf("save node key: %w", err)
	}

	return priv, nil
}
