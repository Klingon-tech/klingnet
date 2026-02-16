import { useState, useEffect, useCallback } from 'react';
import { useWallet } from '../../context/WalletContext';
import { trimAmount, formatTimestamp } from '../../utils/format';
import { StatusGuard } from '@/components/ui/StatusGuard';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import type { TxHistoryEntry } from '../../utils/types';

const PAGE_SIZE = 50;
const MAX_ENTRIES = 1000;
const EXPLORER_URL = 'https://explorer.klingex.io';

function typeBadge(type: string): React.ReactNode {
  switch (type) {
    case 'received':
    case 'mined':
      return (
        <Badge variant="secondary" className="bg-green-100 text-green-700 dark:bg-green-900 dark:text-green-300">
          {type}
        </Badge>
      );
    case 'sent':
      return <Badge variant="destructive">{type}</Badge>;
    case 'staked':
    case 'unstaked':
    case 'register':
      return <Badge variant="secondary">{type === 'register' ? 'sub-chain' : type}</Badge>;
    case 'mint':
      return (
        <Badge variant="secondary" className="bg-yellow-100 text-yellow-700 dark:bg-yellow-900 dark:text-yellow-300">
          {type}
        </Badge>
      );
    case 'token_received':
      return (
        <Badge variant="secondary" className="bg-green-100 text-green-700 dark:bg-green-900 dark:text-green-300">
          {type}
        </Badge>
      );
    case 'token_sent':
      return <Badge variant="destructive">{type}</Badge>;
    default:
      return <Badge variant="outline">{type}</Badge>;
  }
}

function amountDisplay(entry: TxHistoryEntry): { text: string; className: string } {
  if ((entry.type === 'token_received' || entry.type === 'token_sent') && entry.token_amount) {
    const sign = entry.type === 'token_received' ? '+' : '-';
    const cls = entry.type === 'token_received'
      ? 'text-green-600 dark:text-green-400'
      : 'text-red-600 dark:text-red-400'
    return { text: `${sign}${entry.token_amount}`, className: cls };
  }

  const trimmed = trimAmount(entry.amount);
  switch (entry.type) {
    case 'received':
    case 'mined':
    case 'unstaked':
      return { text: `+${trimmed}`, className: 'text-green-600 dark:text-green-400' };
    case 'sent':
    case 'staked':
    case 'register':
      return { text: `-${trimmed}`, className: 'text-red-600 dark:text-red-400' };
    default:
      return { text: trimmed, className: '' };
  }
}

function addressDisplay(entry: TxHistoryEntry): string {
  if (entry.type === 'sent' && entry.to) {
    return entry.to.slice(0, 12) + '...';
  }
  if (entry.type === 'received' && entry.from) {
    return entry.from.slice(0, 12) + '...';
  }
  return '';
}

export default function TransactionList() {
  const { walletName, unlocked, password } = useWallet();
  const [entries, setEntries] = useState<TxHistoryEntry[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const [offset, setOffset] = useState(0);

  const fetchHistory = useCallback(async (off: number) => {
    if (!walletName || !unlocked || !password) return;
    setLoading(true);
    try {
      const mod = await import('../../../wailsjs/go/main/WalletService');
      const result = await mod.GetTransactionHistory(walletName, password, PAGE_SIZE, off);
      if (result) {
        if (off === 0) {
          setEntries(result.entries || []);
        } else {
          setEntries(prev => [...prev, ...(result.entries || [])]);
        }
        setTotal(result.total);
      }
    } catch {
      if (off === 0) setEntries([]);
    } finally {
      setLoading(false);
    }
  }, [walletName, unlocked, password]);

  useEffect(() => {
    setOffset(0);
    fetchHistory(0);
  }, [fetchHistory]);

  // Auto-refresh every 10s.
  useEffect(() => {
    if (!walletName || !unlocked) return;
    const interval = setInterval(() => {
      setOffset(0);
      fetchHistory(0);
    }, 10000);
    return () => clearInterval(interval);
  }, [walletName, unlocked, fetchHistory]);

  const atCap = entries.length >= MAX_ENTRIES;
  const hasMore = !atCap && entries.length < total;

  const loadMore = () => {
    const newOffset = offset + PAGE_SIZE;
    setOffset(newOffset);
    fetchHistory(newOffset);
  };

  return (
    <StatusGuard walletName={walletName} unlocked={unlocked}>
      <div className="space-y-6">
        <Card>
          <CardHeader>
            <CardTitle>Transaction History ({total})</CardTitle>
          </CardHeader>
          <CardContent>
            {loading && entries.length === 0 ? (
              <p className="text-center text-muted-foreground py-8">Loading...</p>
            ) : entries.length === 0 ? (
              <p className="text-center text-muted-foreground py-8">
                No transactions found.
              </p>
            ) : (
              <div className="space-y-4">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Type</TableHead>
                      <TableHead>Amount</TableHead>
                      <TableHead>Address</TableHead>
                      <TableHead>Height</TableHead>
                      <TableHead>Time</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {entries.map((e, i) => {
                      const amt = amountDisplay(e);
                      const addr = addressDisplay(e);
                      return (
                        <TableRow key={`${e.tx_hash}-${i}`}>
                          <TableCell>{typeBadge(e.type)}</TableCell>
                          <TableCell className={`font-mono ${amt.className}`}>{amt.text}</TableCell>
                          <TableCell className="font-mono">{addr}</TableCell>
                          <TableCell>{e.height}</TableCell>
                          <TableCell>{formatTimestamp(e.timestamp)}</TableCell>
                        </TableRow>
                      );
                    })}
                  </TableBody>
                </Table>
                {hasMore && (
                  <div className="flex justify-center pt-2">
                    <Button variant="outline" onClick={loadMore} disabled={loading}>
                      {loading ? 'Loading...' : `Load More (${Math.min(total - entries.length, MAX_ENTRIES - entries.length)} remaining)`}
                    </Button>
                  </div>
                )}
                {atCap && (
                  <div className="text-center pt-2 space-y-1">
                    <p className="text-sm text-muted-foreground">
                      Showing the latest {MAX_ENTRIES.toLocaleString()} transactions.
                    </p>
                    <a
                      href={EXPLORER_URL}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="text-sm text-primary hover:underline"
                    >
                      View full history on Explorer &rarr;
                    </a>
                  </div>
                )}
              </div>
            )}
          </CardContent>
        </Card>
      </div>
    </StatusGuard>
  );
}
