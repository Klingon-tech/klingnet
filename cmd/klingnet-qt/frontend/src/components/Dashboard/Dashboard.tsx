import { useChainInfo, useRecentBlocks } from '../../hooks/useChain';
import { usePolling } from '../../hooks/usePolling';
import { useWallet } from '../../context/WalletContext';
import { truncateHash, formatTimestamp, trimAmount } from '../../utils/format';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { useNavigate } from 'react-router-dom';
import { ArrowUpRight, ArrowDownLeft, Blocks, Users, MemoryStick } from 'lucide-react';
import type { PeersInfo, MempoolInfo } from '../../utils/types';

export default function Dashboard() {
  const navigate = useNavigate();
  const { data: chain, error: chainError } = useChainInfo();
  const { data: blocks } = useRecentBlocks();
  const { walletName, unlocked, accounts, balance } = useWallet();

  const { data: peers } = usePolling<PeersInfo>(async () => {
    const mod = await import('../../../wailsjs/go/main/NetworkService');
    return mod.GetPeers();
  }, 5000);

  const { data: mempool } = usePolling<MempoolInfo>(async () => {
    const mod = await import('../../../wailsjs/go/main/NetworkService');
    return mod.GetMempoolInfo();
  }, 3000);

  if (chainError) {
    return (
      <Alert variant="destructive">
        <AlertDescription>
          Not connected to node. Check Settings to configure the RPC endpoint.
        </AlertDescription>
      </Alert>
    );
  }

  return (
    <div className="space-y-6">
      {/* Hero Balance Card */}
      <Card>
        <CardContent className="flex items-center justify-between py-6">
          <div>
            <p className="text-sm text-muted-foreground mb-1">
              {walletName ? (unlocked ? walletName : `${walletName} (locked)`) : 'No wallet selected'}
            </p>
            <div className="flex items-baseline gap-2">
              <span className="text-4xl font-bold tracking-tight">
                {accounts.length > 0 ? trimAmount(balance.spendable) : '---'}
              </span>
              <span className="text-lg text-muted-foreground">{chain?.symbol || 'KGX'}</span>
            </div>
            {accounts.length > 0 && balance.total !== balance.spendable && (
              <div className="flex gap-4 mt-2 text-xs text-muted-foreground">
                <span>Total: {trimAmount(balance.total)} KGX</span>
                {balance.immature !== '0.000000000000' && (
                  <span>Immature: {trimAmount(balance.immature)}</span>
                )}
                {balance.staked !== '0.000000000000' && (
                  <span>Staked: {trimAmount(balance.staked)}</span>
                )}
                {balance.locked !== '0.000000000000' && (
                  <span>Locked: {trimAmount(balance.locked)}</span>
                )}
              </div>
            )}
          </div>
          <div className="flex gap-2">
            <Button onClick={() => navigate('/send')}>
              <ArrowUpRight className="h-4 w-4 mr-1.5" />
              Send
            </Button>
            <Button variant="outline" onClick={() => navigate('/receive')}>
              <ArrowDownLeft className="h-4 w-4 mr-1.5" />
              Receive
            </Button>
          </div>
        </CardContent>
      </Card>

      {/* Stats Grid */}
      <div className="grid grid-cols-3 gap-4">
        <Card>
          <CardContent className="pt-6">
            <div className="flex items-center gap-2 text-sm text-muted-foreground mb-1">
              <Blocks className="h-4 w-4" />
              Chain Height
            </div>
            <p className="text-2xl font-bold">{chain?.height ?? '---'}</p>
            <p className="text-xs font-mono text-muted-foreground mt-1">
              {chain ? truncateHash(chain.tip_hash) : '---'}
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardContent className="pt-6">
            <div className="flex items-center gap-2 text-sm text-muted-foreground mb-1">
              <Users className="h-4 w-4" />
              Peers
            </div>
            <p className="text-2xl font-bold">{peers?.count ?? '---'}</p>
          </CardContent>
        </Card>

        <Card>
          <CardContent className="pt-6">
            <div className="flex items-center gap-2 text-sm text-muted-foreground mb-1">
              <MemoryStick className="h-4 w-4" />
              Mempool
            </div>
            <p className="text-2xl font-bold">{mempool?.count ?? '---'}</p>
            <p className="text-xs text-muted-foreground mt-1">pending txs</p>
          </CardContent>
        </Card>
      </div>

      {/* Recent Blocks */}
      <Card>
        <CardHeader>
          <CardTitle>Recent Blocks</CardTitle>
        </CardHeader>
        <CardContent>
          {blocks && blocks.length > 0 ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Height</TableHead>
                  <TableHead>Hash</TableHead>
                  <TableHead>Txs</TableHead>
                  <TableHead>Time</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {blocks.map((b) => (
                  <TableRow key={b.height}>
                    <TableCell className="font-medium">{b.height}</TableCell>
                    <TableCell className="font-mono text-muted-foreground">{truncateHash(b.hash)}</TableCell>
                    <TableCell>{b.tx_count}</TableCell>
                    <TableCell className="text-muted-foreground">{formatTimestamp(b.timestamp)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <div className="space-y-2">
              <Skeleton className="h-8 w-full" />
              <Skeleton className="h-8 w-full" />
              <Skeleton className="h-8 w-full" />
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
