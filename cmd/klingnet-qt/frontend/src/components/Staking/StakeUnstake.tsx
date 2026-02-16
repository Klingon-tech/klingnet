import { useState } from 'react';
import { useWallet } from '../../context/WalletContext';
import CopyButton from '../ui/CopyButton';
import { StatusGuard } from '@/components/ui/StatusGuard';
import { DetailRow } from '@/components/ui/DetailRow';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Alert, AlertDescription } from '@/components/ui/alert';
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';

export default function StakeUnstake() {
  const { walletName, unlocked, password, refreshAccounts } = useWallet();
  const [showConfirm, setShowConfirm] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<{ tx_hash: string; amount: string; pubkey: string } | null>(null);

  const handleConfirm = async () => {
    setLoading(true);
    setError(null);
    try {
      const mod = await import('../../../wailsjs/go/main/WalletService');
      const res = await mod.UnstakeTransaction({
        wallet_name: walletName,
        password: password,
      });
      setResult(res);
      setShowConfirm(false);
      refreshAccounts();
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  };

  const reset = () => {
    setError(null);
    setResult(null);
    setShowConfirm(false);
  };

  return (
    <StatusGuard walletName={walletName} unlocked={unlocked}>
      {result ? (
        <Card>
          <CardContent className="pt-6 space-y-4">
            <Alert>
              <AlertDescription>Unstake transaction submitted successfully!</AlertDescription>
            </Alert>
            <DetailRow label="Tx Hash" mono>
              <div className="flex items-center gap-1">
                <span className="break-all">{result.tx_hash}</span>
                <CopyButton text={result.tx_hash} />
              </div>
            </DetailRow>
            <DetailRow label="Returned Amount">{result.amount} KGX</DetailRow>
            <DetailRow label="Validator Public Key" mono>
              <div className="flex items-center gap-1">
                <span className="break-all">{result.pubkey}</span>
                <CopyButton text={result.pubkey} />
              </div>
            </DetailRow>
            <Button onClick={reset}>Done</Button>
          </CardContent>
        </Card>
      ) : (
        <div className="space-y-4">
          {error && (
            <Alert variant="destructive">
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          )}

          <Card>
            <CardHeader>
              <CardTitle>Unstake Validator</CardTitle>
            </CardHeader>
            <CardContent className="space-y-4">
              <p className="text-sm text-muted-foreground">
                Withdraw all staked KGX and deregister as a validator. Returned funds are subject to a cooldown period.
              </p>
              <p className="text-sm text-destructive">
                This will withdraw ALL staked KGX and remove you as a validator.
              </p>
              <Button onClick={() => setShowConfirm(true)}>Review Unstake</Button>
            </CardContent>
          </Card>

          <Dialog open={showConfirm} onOpenChange={(open: boolean) => { if (!open) { setShowConfirm(false); setError(null); } }}>
            <DialogContent>
              <DialogHeader>
                <DialogTitle>Confirm Unstake</DialogTitle>
              </DialogHeader>
              <div className="space-y-1">
                <DetailRow label="Wallet">{walletName}</DetailRow>
              </div>
              {error && (
                <Alert variant="destructive">
                  <AlertDescription>{error}</AlertDescription>
                </Alert>
              )}
              <DialogFooter>
                <Button variant="outline" onClick={() => { setShowConfirm(false); setError(null); }}>Cancel</Button>
                <Button onClick={handleConfirm} disabled={loading}>
                  {loading ? 'Signing...' : 'Unstake'}
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>
        </div>
      )}
    </StatusGuard>
  );
}
