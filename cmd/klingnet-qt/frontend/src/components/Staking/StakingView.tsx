import { useState } from 'react';
import { usePolling } from '../../hooks/usePolling';
import { truncateHash, trimAmount } from '../../utils/format';
import CopyButton from '../ui/CopyButton';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { DetailRow } from '@/components/ui/DetailRow';
import type { ValidatorsInfo, StakeDetail } from '../../utils/types';

export default function StakingView() {
  const [selectedPubkey, setSelectedPubkey] = useState<string | null>(null);
  const [stakeDetail, setStakeDetail] = useState<StakeDetail | null>(null);

  const { data: validators, error } = usePolling<ValidatorsInfo>(async () => {
    const mod = await import('../../../wailsjs/go/main/StakingService');
    return mod.GetValidators();
  }, 5000);

  const handleViewStake = async (pubkey: string) => {
    try {
      const mod = await import('../../../wailsjs/go/main/StakingService');
      const detail = await mod.GetStakeInfo(pubkey);
      setStakeDetail(detail);
      setSelectedPubkey(pubkey);
    } catch {
      // ignore
    }
  };

  if (error) {
    return (
      <Alert variant="destructive">
        <AlertDescription>Unable to load validators: {error}</AlertDescription>
      </Alert>
    );
  }

  return (
    <div className="space-y-6">
      <div className="grid grid-cols-2 gap-4">
        <Card>
          <CardContent className="pt-6">
            <p className="text-sm text-muted-foreground mb-1">Validators</p>
            <p className="text-2xl font-bold">{validators?.validators.length ?? '---'}</p>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-6">
            <p className="text-sm text-muted-foreground mb-1">Min Stake</p>
            <p className="text-xl font-bold">
              {validators ? trimAmount(validators.min_stake) : '---'} KGX
            </p>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Validator List</CardTitle>
        </CardHeader>
        <CardContent>
          {validators && validators.validators.length > 0 ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>#</TableHead>
                  <TableHead>Public Key</TableHead>
                  <TableHead>Type</TableHead>
                  <TableHead></TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {validators.validators.map((v, i) => (
                  <TableRow key={i}>
                    <TableCell className="font-medium">{i}</TableCell>
                    <TableCell>
                      <div className="flex items-center gap-1 font-mono">
                        <span>{truncateHash(v.pubkey, 12)}</span>
                        <CopyButton text={v.pubkey} />
                      </div>
                    </TableCell>
                    <TableCell>
                      {v.is_genesis ? (
                        <Badge className="bg-green-100 text-green-700 dark:bg-green-900 dark:text-green-300">Genesis</Badge>
                      ) : (
                        <Badge variant="secondary">Staked</Badge>
                      )}
                    </TableCell>
                    <TableCell>
                      <Button variant="outline" size="sm" onClick={() => handleViewStake(v.pubkey)}>
                        Details
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <p className="text-sm text-muted-foreground">Loading validators...</p>
          )}
        </CardContent>
      </Card>

      {selectedPubkey && stakeDetail && (
        <Card>
          <CardHeader>
            <CardTitle>Stake Details</CardTitle>
          </CardHeader>
          <CardContent className="space-y-1">
            <DetailRow label="Public Key" mono>
              <div className="flex items-center gap-1">
                <span className="break-all">{stakeDetail.pubkey}</span>
                <CopyButton text={stakeDetail.pubkey} />
              </div>
            </DetailRow>
            <DetailRow label="Total Stake">{trimAmount(stakeDetail.total_stake)} KGX</DetailRow>
            <DetailRow label="Min Stake">{trimAmount(stakeDetail.min_stake)} KGX</DetailRow>
            <DetailRow label="Genesis Validator">{stakeDetail.is_genesis ? 'Yes' : 'No'}</DetailRow>
            <div className="pt-3">
              <Button variant="outline" size="sm" onClick={() => { setSelectedPubkey(null); setStakeDetail(null); }}>
                Close
              </Button>
            </div>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
