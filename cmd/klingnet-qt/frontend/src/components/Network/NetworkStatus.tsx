import { useState } from 'react';
import { usePolling } from '../../hooks/usePolling';
import { truncateHash } from '../../utils/format';
import CopyButton from '../ui/CopyButton';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import type { NodeInfo, PeersInfo, MempoolInfo, MempoolContent } from '../../utils/types';

export default function NetworkStatus() {
  const [showAddrs, setShowAddrs] = useState(false);
  const { data: node, error: nodeError } = usePolling<NodeInfo>(async () => {
    const mod = await import('../../../wailsjs/go/main/NetworkService');
    return mod.GetNodeInfo();
  }, 5000);

  const { data: peers } = usePolling<PeersInfo>(async () => {
    const mod = await import('../../../wailsjs/go/main/NetworkService');
    return mod.GetPeers();
  }, 5000);

  const { data: mempool } = usePolling<MempoolInfo>(async () => {
    const mod = await import('../../../wailsjs/go/main/NetworkService');
    return mod.GetMempoolInfo();
  }, 3000);

  const { data: mempoolContent } = usePolling<MempoolContent>(async () => {
    const mod = await import('../../../wailsjs/go/main/NetworkService');
    return mod.GetMempoolContent();
  }, 3000);

  if (nodeError) {
    return (
      <Alert variant="destructive">
        <AlertDescription>Unable to connect to node: {nodeError}</AlertDescription>
      </Alert>
    );
  }

  return (
    <div className="space-y-6">
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        <Card>
          <CardHeader>
            <CardTitle>Node ID</CardTitle>
          </CardHeader>
          <CardContent className="space-y-2">
            <div className="flex items-start gap-2">
              <span className="font-mono text-sm break-all min-w-0 flex-1">
                {node?.id || '---'}
              </span>
              {node?.id && <CopyButton text={node.id} />}
            </div>
            {node?.addrs && node.addrs.length > 0 && (
              <div className="space-y-1">
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-auto px-0 py-0 text-xs text-muted-foreground hover:text-foreground"
                  onClick={() => setShowAddrs(!showAddrs)}
                >
                  {showAddrs ? 'Hide' : 'Show'} listen addresses ({node.addrs.length})
                </Button>
                {showAddrs && node.addrs.map((a, i) => (
                  <p key={i} className="text-xs text-muted-foreground break-all">{a}</p>
                ))}
              </div>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Mempool</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-3xl font-bold">{mempool?.count ?? '---'}</p>
            <p className="mt-2 text-xs text-muted-foreground">
              Min fee rate: {mempool?.min_fee_rate ?? '---'} base units/byte
            </p>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Connected Peers ({peers?.count ?? 0})</CardTitle>
        </CardHeader>
        <CardContent>
          {peers && peers.peers.length > 0 ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Peer ID</TableHead>
                  <TableHead>Connected At</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {peers.peers.map((p, i) => (
                  <TableRow key={i}>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <span className="font-mono">{truncateHash(p.id, 12)}</span>
                        <CopyButton text={p.id} />
                      </div>
                    </TableCell>
                    <TableCell>{p.connected_at}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <p className="text-center text-muted-foreground py-4">
              No peers connected.
            </p>
          )}
        </CardContent>
      </Card>

      {mempoolContent && mempoolContent.hashes && mempoolContent.hashes.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle>Pending Transactions</CardTitle>
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Tx Hash</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {mempoolContent.hashes.map((h, i) => (
                  <TableRow key={i}>
                    <TableCell className="font-mono">{h}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
