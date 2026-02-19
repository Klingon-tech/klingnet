import type { TxHistoryEntry } from '../utils/types';

function compactHash(hash: string): string {
  return hash.length > 16 ? `${hash.slice(0, 10)}...${hash.slice(-6)}` : hash;
}

export function shouldNotifyEntry(entry: TxHistoryEntry, changeAddrs: Set<string>): boolean {
  switch (entry.type) {
    case 'sent':
    case 'token_sent':
    case 'mined':
      return true;
    case 'received':
    case 'token_received':
      return !!entry.to && !changeAddrs.has(entry.to);
    default:
      return false;
  }
}

export function entryNotificationKey(entry: TxHistoryEntry): string {
  return `${entry.type}:${entry.tx_hash}`;
}

export async function notifyEntry(entry: TxHistoryEntry): Promise<void> {
  let title = 'Transaction update';
  let body = compactHash(entry.tx_hash);

  if (entry.type === 'mined') {
    title = 'Block Reward';
    body = `Mined ${entry.amount} KGX`;
  } else if (entry.type === 'sent') {
    title = 'KGX Sent';
    body = `${entry.amount} KGX`;
    if (entry.to) body += ` to ${entry.to.slice(0, 14)}...`;
  } else if (entry.type === 'received') {
    title = 'KGX Received';
    body = `${entry.amount} KGX`;
    if (entry.from) body += ` from ${entry.from.slice(0, 14)}...`;
  } else if (entry.type === 'token_sent') {
    title = 'Token Sent';
    body = entry.token_amount ? `${entry.token_amount} units` : compactHash(entry.tx_hash);
  } else if (entry.type === 'token_received') {
    title = 'Token Received';
    body = entry.token_amount ? `${entry.token_amount} units` : compactHash(entry.tx_hash);
  }

  try {
    const mod = await import('../../wailsjs/go/main/App');
    await mod.SendNotification(title, body);
  } catch {
    // Wails binding unavailable — ignore silently.
  }
}
