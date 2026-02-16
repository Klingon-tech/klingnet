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

const STAKE_AMOUNT = '2000';

export default function StakeCreate() {
  const { walletName, unlocked, password, refreshAccounts } = useWallet();
  const [showConfirm, setShowConfirm] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<{ tx_hash: string; pubkey: string } | null>(null);

  const handleConfirm = async () => {
    setLoading(true);
    setError(null);
    try {
      const mod = await import('../../../wailsjs/go/main/WalletService');
      const res = await mod.StakeTransaction({
        wallet_name: walletName,
        password: password,
        amount: STAKE_AMOUNT,
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
              <AlertDescription>Stake transaction submitted successfully!</AlertDescription>
            </Alert>
            <DetailRow label="Tx Hash" mono>
              <div className="flex items-center gap-1">
                <span className="break-all">{result.tx_hash}</span>
                <CopyButton text={result.tx_hash} />
              </div>
            </DetailRow>
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
              <CardTitle>Create Validator Stake</CardTitle>
            </CardHeader>
            <CardContent className="space-y-4">
              <p className="text-sm text-muted-foreground">
                Lock exactly {STAKE_AMOUNT} KGX to register as a block validator.
              </p>
              <div className="space-y-1">
                <DetailRow label="Stake Amount">{STAKE_AMOUNT} KGX</DetailRow>
                <DetailRow label="Wallet">{walletName}</DetailRow>
              </div>
              <Button onClick={() => setShowConfirm(true)}>Review Stake</Button>
            </CardContent>
          </Card>

          <Dialog open={showConfirm} onOpenChange={(open: boolean) => { if (!open) { setShowConfirm(false); setError(null); } }}>
            <DialogContent>
              <DialogHeader>
                <DialogTitle>Confirm Stake</DialogTitle>
              </DialogHeader>
              <div className="space-y-1">
                <DetailRow label="Wallet">{walletName}</DetailRow>
                <DetailRow label="Stake Amount">{STAKE_AMOUNT} KGX</DetailRow>
              </div>
              <p className="text-xs text-muted-foreground">
                This will lock {STAKE_AMOUNT} KGX. You can withdraw your stake later.
              </p>
              {error && (
                <Alert variant="destructive">
                  <AlertDescription>{error}</AlertDescription>
                </Alert>
              )}
              <DialogFooter>
                <Button variant="outline" onClick={() => { setShowConfirm(false); setError(null); }}>Cancel</Button>
                <Button onClick={handleConfirm} disabled={loading}>
                  {loading ? 'Signing...' : 'Stake'}
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>
        </div>
      )}
    </StatusGuard>
  );
}
