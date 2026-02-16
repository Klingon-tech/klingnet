// Truncate a hex hash for display: "abcdef1234...5678"
export function truncateHash(hash: string, chars = 8): string {
  if (hash.length <= chars * 2 + 3) return hash;
  return hash.slice(0, chars) + '...' + hash.slice(-chars);
}

// Format a Unix timestamp to locale string.
export function formatTimestamp(ts: number): string {
  if (ts === 0) return 'Genesis';
  return new Date(ts * 1000).toLocaleString();
}

// Trim trailing zeros from an amount string, keeping at least 2 decimals.
export function trimAmount(amount: string): string {
  const [whole, frac] = amount.split('.');
  if (!frac) return amount;
  // Keep at least 2 decimal places, trim trailing zeros beyond that.
  let trimmed = frac.replace(/0+$/, '');
  if (trimmed.length < 2) trimmed = trimmed.padEnd(2, '0');
  return `${whole}.${trimmed}`;
}

// Script type to human-readable string.
export function scriptTypeName(type_: number): string {
  switch (type_) {
    case 0x01: return 'P2PKH';
    case 0x02: return 'P2SH';
    case 0x10: return 'Mint';
    case 0x11: return 'Burn';
    case 0x20: return 'Anchor';
    case 0x21: return 'Register';
    case 0x30: return 'Bridge';
    case 0x40: return 'Stake';
    default: return `0x${type_.toString(16).padStart(2, '0')}`;
  }
}
