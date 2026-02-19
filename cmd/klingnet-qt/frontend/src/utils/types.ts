// Go service types mirrored for TypeScript

export interface ChainInfo {
  chain_id: string;
  symbol: string;
  height: number;
  tip_hash: string;
}

export interface BlockInfo {
  hash: string;
  prev_hash: string;
  merkle_root: string;
  timestamp: number;
  height: number;
  validator_sig?: string;
  tx_count: number;
  transactions: TxBrief[];
}

export interface TxBrief {
  hash: string;
  version: number;
  input_count: number;
  outputs: TxOutput[];
  is_coinbase: boolean;
}

export interface TxOutput {
  value: string;
  script_type: number;
  script_data: string;
}

export interface TxInfo {
  hash: string;
  version: number;
  inputs: TxInput[];
  outputs: TxOutput[];
  locktime: number;
}

export interface TxInput {
  tx_id: string;
  index: number;
}

export interface BlockSummary {
  height: number;
  hash: string;
  timestamp: number;
  tx_count: number;
}

export interface WalletInfo {
  name: string;
  address: string;
}

export interface BalanceInfo {
  total: string;
  spendable: string;
  immature: string;
  staked: string;
  locked: string;
}

export interface AccountInfo {
  index: number;
  change: number;
  name: string;
  address: string;
}

export interface UTXOInfo {
  tx_id: string;
  index: number;
  value: string;
  script_type: number;
}

export interface SendRequest {
  wallet_name: string;
  password: string;
  to_address: string;
  amount: string;
}

export interface SendResult {
  tx_hash: string;
}

export interface SendManyRecipient {
  to_address: string;
  amount: string;
}

export interface SendManyRequest {
  wallet_name: string;
  password: string;
  recipients: SendManyRecipient[];
}

export interface SendManyResult {
  tx_hash: string;
}

export interface NodeInfo {
  id: string;
  addrs: string[];
}

export interface PeerEntry {
  id: string;
  connected_at: string;
}

export interface PeersInfo {
  count: number;
  peers: PeerEntry[];
}

export interface MempoolInfo {
  count: number;
  min_fee_rate: string;
}

export interface MempoolContent {
  hashes: string[];
}

export interface ValidatorInfo {
  pubkey: string;
  is_genesis: boolean;
}

export interface ValidatorsInfo {
  min_stake: string;
  validators: ValidatorInfo[];
}

export interface StakeDetail {
  pubkey: string;
  total_stake: string;
  min_stake: string;
  sufficient: boolean;
  is_genesis: boolean;
}

export interface StakeRequest {
  wallet_name: string;
  password: string;
  amount: string;
}

export interface StakeResult {
  tx_hash: string;
  pubkey: string;
}

export interface UnstakeRequest {
  wallet_name: string;
  password: string;
}

export interface UnstakeResult {
  tx_hash: string;
  amount: string;
  pubkey: string;
}

export interface ExportKeyResult {
  private_key: string;
  pubkey: string;
  address: string;
}

export interface MintTokenRequest {
  wallet_name: string;
  password: string;
  token_name: string;
  symbol: string;
  decimals: number;
  amount: string;
  recipient: string;
}

export interface MintTokenResult {
  tx_hash: string;
  token_id: string;
}

export interface SubChainEntry {
  chain_id: string;
  name: string;
  symbol: string;
  consensus_type: string;
  syncing: boolean;
  height: number;
  created_at: number;
  balance: string;
}

export interface SubChainDetail {
  chain_id: string;
  name: string;
  symbol: string;
  consensus_type: string;
  syncing: boolean;
  height: number;
  tip_hash: string;
  created_at: number;
  registration_tx: string;
  initial_difficulty?: number;
  difficulty_adjust?: number;
  current_difficulty?: number;
}

export interface SubChainListInfo {
  count: number;
  chains: SubChainEntry[];
}

export interface CreateSubChainRequest {
  wallet_name: string;
  password: string;
  chain_name: string;
  symbol: string;
  consensus_type: string;
  block_time: number;
  block_reward: string;
  max_supply: string;
  min_fee_rate: string;
  validators?: string[];
  initial_difficulty?: number;
  difficulty_adjust?: number;
}

export interface CreateSubChainResult {
  tx_hash: string;
  chain_id: string;
}

export interface SubChainSendRequest {
  chain_id: string;
  wallet_name: string;
  password: string;
  to_address: string;
  amount: string;
}

export interface SubChainSendResult {
  tx_hash: string;
}

export interface SubChainStakeRequest {
  chain_id: string;
  wallet_name: string;
  password: string;
  amount: string;
}

export interface SubChainStakeResult {
  tx_hash: string;
  pubkey: string;
}

export interface SubChainUnstakeRequest {
  chain_id: string;
  wallet_name: string;
  password: string;
}

export interface SubChainUnstakeResult {
  tx_hash: string;
  amount: string;
  pubkey: string;
}

export interface NewAddressResult {
  index: number;
  address: string;
}

export interface NotificationSettings {
  mined: boolean;
  sent: boolean;
  received: boolean;
  token_sent: boolean;
  token_received: boolean;
}

export interface TxHistoryEntry {
  tx_hash: string;
  block_hash: string;
  height: number;
  timestamp: number;
  type: string;
  amount: string;
  fee: string;
  to?: string;
  from?: string;
  confirmed: boolean;
  token_id?: string;
  token_amount?: string;
}

export interface TxHistoryResult {
  total: number;
  entries: TxHistoryEntry[];
}
