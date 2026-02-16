// Type declarations for Wails Go bindings.
// At runtime, Wails injects window.go with bound methods.
// During dev, we call these via the wails runtime bridge.

declare module '*/wailsjs/go/main/App' {
  export function GetRPCEndpoint(): Promise<string>;
  export function SetRPCEndpoint(endpoint: string): Promise<void>;
  export function GetDataDir(): Promise<string>;
  export function SetDataDir(dir: string): Promise<void>;
  export function GetNetwork(): Promise<string>;
  export function SetNetwork(network: string): Promise<void>;
  export function TestConnection(): Promise<boolean>;
  export function GetActiveWallet(): Promise<string>;
  export function SetActiveWallet(name: string): Promise<void>;
  export function GetKnownAccounts(walletName: string): Promise<import('../utils/types').AccountInfo[]>;
  export function SetKnownAccounts(walletName: string, accounts: import('../utils/types').AccountInfo[]): Promise<void>;
}

declare module '*/wailsjs/go/main/WalletService' {
  import type { WalletInfo, AccountInfo, UTXOInfo, SendRequest, SendResult,
    SendManyRequest, SendManyResult,
    StakeRequest, StakeResult, UnstakeRequest, UnstakeResult,
    MintTokenRequest, MintTokenResult, ExportKeyResult } from '../utils/types';
  export function GenerateMnemonic(): Promise<string>;
  export function ValidateMnemonic(mnemonic: string): Promise<boolean>;
  export function CreateWallet(name: string, password: string, mnemonic: string): Promise<WalletInfo>;
  export function ImportWallet(name: string, password: string, mnemonic: string): Promise<WalletInfo>;
  export function ListWallets(): Promise<string[]>;
  export function GetWalletAccounts(name: string, password: string): Promise<AccountInfo[]>;
  export function DeleteWallet(name: string): Promise<void>;
  export function GetBalance(address: string): Promise<string>;
  export function GetTotalBalance(addresses: string[]): Promise<string>;
  export function GetUTXOs(address: string): Promise<UTXOInfo[]>;
  export function SendTransaction(req: SendRequest): Promise<SendResult>;
  export function SendManyTransaction(req: SendManyRequest): Promise<SendManyResult>;
  export function EstimateFee(): Promise<string>;
  export function StakeTransaction(req: StakeRequest): Promise<StakeResult>;
  export function UnstakeTransaction(req: UnstakeRequest): Promise<UnstakeResult>;
  export function MintToken(req: MintTokenRequest): Promise<MintTokenResult>;
  export function ExportValidatorKey(name: string, password: string, account: number, index: number): Promise<ExportKeyResult>;
}

declare module '*/wailsjs/go/main/ChainService' {
  import type { ChainInfo, BlockInfo, BlockSummary, TxInfo } from '../utils/types';
  export function GetChainInfo(): Promise<ChainInfo>;
  export function GetBlockByHeight(height: number): Promise<BlockInfo>;
  export function GetBlockByHash(hash: string): Promise<BlockInfo>;
  export function GetTransaction(hash: string): Promise<TxInfo>;
  export function GetRecentBlocks(count: number): Promise<BlockSummary[]>;
  export function GetBlockRange(from: number, to: number): Promise<BlockSummary[]>;
}

declare module '*/wailsjs/go/main/NetworkService' {
  import type { NodeInfo, PeersInfo, MempoolInfo, MempoolContent } from '../utils/types';
  export function GetNodeInfo(): Promise<NodeInfo>;
  export function GetPeers(): Promise<PeersInfo>;
  export function GetMempoolInfo(): Promise<MempoolInfo>;
  export function GetMempoolContent(): Promise<MempoolContent>;
}

declare module '*/wailsjs/go/main/StakingService' {
  import type { ValidatorsInfo, StakeDetail } from '../utils/types';
  export function GetValidators(): Promise<ValidatorsInfo>;
  export function GetStakeInfo(pubkey: string): Promise<StakeDetail>;
}

declare module '*/wailsjs/go/main/SubChainService' {
  import type { SubChainListInfo, SubChainDetail } from '../utils/types';
  export function ListSubChains(): Promise<SubChainListInfo>;
  export function GetSubChainInfo(chainID: string): Promise<SubChainDetail>;
}
