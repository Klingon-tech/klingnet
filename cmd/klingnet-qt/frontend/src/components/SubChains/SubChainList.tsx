import { useState, useEffect, useRef } from 'react';
import { useNavigate } from 'react-router-dom';
import { useWallet } from '../../context/WalletContext';
import { usePolling } from '../../hooks/usePolling';
import { truncateHash, formatTimestamp } from '../../utils/format';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { DetailRow } from '@/components/ui/DetailRow';
import CopyButton from '../ui/CopyButton';
import type { SubChainListInfo, SubChainDetail } from '../../utils/types';

export default function SubChainList() {
  const navigate = useNavigate();
  const { accounts } = useWallet();
  const [selectedChainID, setSelectedChainID] = useState<string | null>(null);
  const [detail, setDetail] = useState<SubChainDetail | null>(null);
  const selectedRef = useRef(selectedChainID);
  selectedRef.current = selectedChainID;

  const addresses = accounts.map((a) => a.address);

  const { data: list, error } = usePolling<SubChainListInfo>(async () => {
    const mod = await import('../../../wailsjs/go/main/SubChainService');
    return mod.ListSubChains(addresses);
  }, 5000);

  // Poll detail card when a chain is selected.
  useEffect(() => {
    if (!selectedChainID) {
      setDetail(null);
      return;
    }
    const fetchDetail = async () => {
      try {
        const mod = await import('../../../wailsjs/go/main/SubChainService');
        const d = await mod.GetSubChainInfo(selectedRef.current!);
        setDetail(d);
      } catch {
        // ignore
      }
    };
    fetchDetail();
    const id = setInterval(fetchDetail, 5000);
    return () => clearInterval(id);
  }, [selectedChainID]);

  if (error) {
    return (
      <Alert variant="destructive">
        <AlertDescription>Unable to load sub-chains: {error}</AlertDescription>
      </Alert>
    );
  }

  const trimBalance = (bal: string) => {
    // Remove trailing zeros after decimal point for cleaner display.
    if (!bal.includes('.')) return bal;
    const trimmed = bal.replace(/0+$/, '').replace(/\.$/, '');
    return trimmed || '0';
  };

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <CardTitle>Sub-Chains ({list?.count ?? 0})</CardTitle>
            <Button size="sm" onClick={() => navigate('/subchain-create')}>
              Create Sub-Chain
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          {list && list.chains.length > 0 ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Symbol</TableHead>
                  <TableHead>Consensus</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Height</TableHead>
                  <TableHead>Balance</TableHead>
                  <TableHead></TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {list.chains.map((sc) => (
                  <TableRow key={sc.chain_id}>
                    <TableCell>{sc.name}</TableCell>
                    <TableCell>{sc.symbol}</TableCell>
                    <TableCell className="uppercase">{sc.consensus_type}</TableCell>
                    <TableCell>
                      {sc.syncing ? (
                        <Badge className="bg-green-100 text-green-700 dark:bg-green-900 dark:text-green-300">Syncing</Badge>
                      ) : (
                        <Badge className="bg-yellow-100 text-yellow-700 dark:bg-yellow-900 dark:text-yellow-300">Not syncing</Badge>
                      )}
                    </TableCell>
                    <TableCell>{sc.syncing ? sc.height : '---'}</TableCell>
                    <TableCell>{sc.syncing ? `${trimBalance(sc.balance)} ${sc.symbol}` : '---'}</TableCell>
                    <TableCell>
                      <Button variant="outline" size="sm" onClick={() => setSelectedChainID(sc.chain_id)}>
                        Details
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <div className="text-muted-foreground py-5 text-center">
              No sub-chains registered.
            </div>
          )}
        </CardContent>
      </Card>

      {detail && (
        <Card>
          <CardHeader>
            <CardTitle>Sub-Chain Details</CardTitle>
          </CardHeader>
          <CardContent className="space-y-0">
            <DetailRow label="Chain ID" mono>
              <div className="flex items-center gap-1">
                <span className="break-all">{detail.chain_id}</span>
                <CopyButton text={detail.chain_id} />
              </div>
            </DetailRow>
            <DetailRow label="Name">{detail.name}</DetailRow>
            <DetailRow label="Symbol">{detail.symbol}</DetailRow>
            <DetailRow label="Consensus">
              <span className="uppercase">{detail.consensus_type}</span>
            </DetailRow>
            {detail.consensus_type === 'pow' && (
              <>
                <DetailRow label="Current Difficulty">{detail.current_difficulty ?? detail.initial_difficulty ?? 0}</DetailRow>
                <DetailRow label="Initial Difficulty">{detail.initial_difficulty ?? 0}</DetailRow>
                <DetailRow label="Difficulty Adjust">
                  {(detail.difficulty_adjust ?? 0) > 0
                    ? `Every ${detail.difficulty_adjust} blocks`
                    : 'Disabled'}
                </DetailRow>
              </>
            )}
            <DetailRow label="Syncing">{detail.syncing ? 'Yes' : 'No'}</DetailRow>
            {detail.syncing && (
              <>
                <DetailRow label="Height">{detail.height}</DetailRow>
                <DetailRow label="Tip Hash" mono>
                  <div className="flex items-center gap-1">
                    <span className="break-all">{detail.tip_hash}</span>
                    <CopyButton text={detail.tip_hash} />
                  </div>
                </DetailRow>
              </>
            )}
            <DetailRow label="Created At">{formatTimestamp(detail.created_at)}</DetailRow>
            <DetailRow label="Registration Tx" mono>
              <div className="flex items-center gap-1">
                <span className="break-all">{truncateHash(detail.registration_tx)}</span>
                <CopyButton text={detail.registration_tx} />
              </div>
            </DetailRow>
            <div className="pt-3">
              <Button variant="outline" size="sm" onClick={() => setSelectedChainID(null)}>
                Close
              </Button>
            </div>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
