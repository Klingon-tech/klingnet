import { usePolling } from './usePolling';
import type { ChainInfo, BlockSummary } from '../utils/types';

// Placeholder functions that will be replaced by Wails bindings at runtime.
// During development, these are injected by Wails dev server.
async function getChainInfo(): Promise<ChainInfo> {
  const mod = await import('../../wailsjs/go/main/ChainService');
  return mod.GetChainInfo();
}

async function getRecentBlocks(): Promise<BlockSummary[]> {
  const mod = await import('../../wailsjs/go/main/ChainService');
  return mod.GetRecentBlocks(5);
}

export function useChainInfo() {
  return usePolling<ChainInfo>(getChainInfo, 3000);
}

export function useRecentBlocks() {
  return usePolling<BlockSummary[]>(getRecentBlocks, 3000);
}
