# Klingnet Mainnet Setup Guide

## Prerequisites

Build the binaries:

```bash
go build -o klingnetd ./cmd/klingnetd
go build -o klingnet-cli ./cmd/klingnet-cli
```

Copy both binaries to your VPS (e.g. into `~/kgx/`).

---

## Step 1: Start the node (first run auto-creates data dirs)

On first start, klingnetd automatically creates `~/.klingnet/` with config and data directories.

---

## Step 2: Create a wallet

```bash
./klingnet-cli --network mainnet wallet create --name validator1
```

This will:
1. Ask for a password (used to encrypt the wallet)
2. Print your **24-word mnemonic** -- write it down and store it safely
3. Print your address

---

## Step 3: Export the validator key

```bash
./klingnet-cli --network mainnet wallet export-key --wallet validator1 --account 0
```

This creates a `.key` file (e.g. `validator1.key`) containing your hex-encoded private key.

Output:
```
Exported validator key to: validator1.key
  Path:    m/44'/8888'/0'/0/0
  PubKey:  03854c6c...
  Address: kgx:ab19b376...
```

**IMPORTANT:** Keep `validator1.key` safe and private. It contains your private key.

Move it to a secure location:
```bash
mv validator1.key ~/.klingnet/validator.key
chmod 600 ~/.klingnet/validator.key
```

---

## Step 4: Start the node

### As a validator (produces blocks):

```bash
./klingnetd \
  --network mainnet \
  --mine \
  --validator-key ~/.klingnet/validator.key \
  --dht-server
```

Notes:
- `--validator-key` takes the **path to the .key file**, not a raw pubkey
- `--coinbase` is optional -- if omitted, block rewards go to the address derived from the validator key
- `--dht-server` makes this node a DHT server (recommended for validators and seed nodes)

### As a non-mining node (sync only):

```bash
./klingnetd \
  --network mainnet \
  --seeds "/ip4/<SEED_IP>/tcp/30303/p2p/<PEER_ID>"
```

---

## Step 5: Verify the node is running

In another terminal:

```bash
./klingnet-cli --network mainnet status
./klingnet-cli --network mainnet balance --address <your_address>
```

---

## Connecting additional nodes

When the first node starts, it prints its peer ID in the logs:

```
P2P node started, peer ID: 12D3KooW...
```

Use this to connect other nodes:

```bash
./klingnetd \
  --network mainnet \
  --seeds "/ip4/<FIRST_NODE_IP>/tcp/30303/p2p/12D3KooW..."
```

To add more validators later, stake 2,000 KGX (mainnet) from any node:

```bash
./klingnet-cli --network mainnet stake create \
  --wallet validator2 \
  --amount 2000
```

---

## Default ports

| Service | Mainnet | Testnet |
|---------|---------|---------|
| P2P     | 30303   | 30304   |
| RPC     | 8545    | 8645    |

RPC binds to `127.0.0.1` by default (localhost only).

---

## Data directory layout

```
~/.klingnet/
  klingnet.conf              # Node configuration
  qt-settings.json           # Qt GUI settings (if using klingnet-qt)
  logs/                      # Log files
  mainnet/
    chain/                   # Single BadgerDB (blocks, UTXOs, tx index, state)
    keystore/                # Encrypted wallet files (.wallet)
    subchains/               # Sub-chain registry
```

---

## Useful commands

```bash
# Node status
./klingnet-cli --network mainnet status

# Check balance
./klingnet-cli --network mainnet balance --address <address>

# List wallets
./klingnet-cli --network mainnet wallet list

# Send KGX
./klingnet-cli --network mainnet send \
  --wallet validator1 \
  --to <recipient_address> \
  --amount 10.5

# View connected peers
./klingnet-cli --network mainnet peers

# View validators
./klingnet-cli --network mainnet validators
```

---

## Chain parameters (from genesis)

| Parameter         | Value           |
|-------------------|-----------------|
| Symbol            | KGX             |
| Max supply        | 2,000,000 KGX  |
| Block reward      | 0.02 KGX       |
| Block time        | 3 seconds       |
| Min fee           | 0.000001 KGX   |
| Validator stake   | 2,000 KGX      |
| Coinbase maturity | 20 blocks       |
| Sub-chain deposit | 1,000 KGX (burn) |
