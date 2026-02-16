import { useState } from 'react';
import { useWallet } from '../../context/WalletContext';
import CopyButton from '../ui/CopyButton';
import { StatusGuard } from '@/components/ui/StatusGuard';
import { DetailRow } from '@/components/ui/DetailRow';
import { Card, CardContent } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Alert, AlertDescription } from '@/components/ui/alert';
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';

function validateAddress(addr: string): string | null {
  if (!addr) return 'Enter recipient address';
  if (/^(kgx|tkgx)1[qpzry9x8gf2tvdw0s3jn54khce6mua7l]{30,}$/.test(addr.toLowerCase())) {
    return null;
  }
  if (/^[0-9a-fA-F]{40}$/.test(addr)) {
    return null;
  }
  return 'Invalid address. Expected bech32 format (kgx1...)';
}

function validateAmount(s: string): string | null {
  if (!s) return 'Enter amount';
  const n = parseFloat(s);
  if (isNaN(n) || n <= 0) return 'Amount must be a positive number';
  return null;
}

export default function Send() {
  const { walletName, unlocked, password, refreshAccounts } = useWallet();
  const [toAddress, setToAddress] = useState('');
  const [amount, setAmount] = useState('');
  const [showConfirm, setShowConfirm] = useState(false);
  const [loading, setLoading] = useState(false);
  const [consolidating, setConsolidating] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [txHash, setTxHash] = useState<string | null>(null);
  const [consolidateNote, setConsolidateNote] = useState<string | null>(null);

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const addrErr = validateAddress(toAddress);
    if (addrErr) { setError(addrErr); return; }
    const amtErr = validateAmount(amount);
    if (amtErr) { setError(amtErr); return; }
    setError(null);
    setShowConfirm(true);
  };

  const handleConfirm = async () => {
    setLoading(true);
    setError(null);
    try {
      const mod = await import('../../../wailsjs/go/main/WalletService');
      const result = await mod.SendTransaction({
        wallet_name: walletName,
        password: password,
        to_address: toAddress,
        amount: amount,
      });
      setTxHash(result.tx_hash);
      setShowConfirm(false);
      refreshAccounts();
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  };

  const handleConsolidate = async () => {
    setConsolidating(true);
    setError(null);
    setConsolidateNote(null);
    try {
      const mod = await import('../../../wailsjs/go/main/WalletService');
      const result = await mod.ConsolidateUTXOs(walletName, password, 500);
      setConsolidateNote(
        `Consolidation submitted: ${result.inputs_used} inputs -> ${result.output_amount} KGX (fee ${result.fee}). Tx: ${result.tx_hash}`,
      );
      refreshAccounts();
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setConsolidating(false);
    }
  };

  const reset = () => {
    setToAddress('');
    setAmount('');
    setError(null);
    setTxHash(null);
    setShowConfirm(false);
  };

  return (
    <StatusGuard walletName={walletName} unlocked={unlocked}>
      {txHash ? (
        <Card>
          <CardContent className="pt-6 space-y-4">
            <Alert>
              <AlertDescription>Transaction submitted successfully!</AlertDescription>
            </Alert>
            <DetailRow label="Tx Hash" mono>
              <div className="flex items-center gap-1">
                <span className="break-all">{txHash}</span>
                <CopyButton text={txHash} />
              </div>
            </DetailRow>
            <Button onClick={reset}>Send Another</Button>
          </CardContent>
        </Card>
      ) : (
        <div className="space-y-4">
          {error && (
            <Alert variant="destructive">
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          )}
          {consolidateNote && (
            <Alert>
              <AlertDescription className="font-mono text-xs break-all">{consolidateNote}</AlertDescription>
            </Alert>
          )}
          <Card>
            <CardContent className="pt-6">
              <form onSubmit={handleSubmit} className="space-y-4">
                <div className="space-y-2">
                  <Label>Recipient Address</Label>
                  <Input
                    value={toAddress}
                    onChange={(e: React.ChangeEvent<HTMLInputElement>) => setToAddress(e.target.value)}
                    placeholder="kgx1..."
                    className="font-mono"
                  />
                </div>
                <div className="space-y-2">
                  <Label>Amount (KGX)</Label>
                  <Input
                    value={amount}
                    onChange={(e: React.ChangeEvent<HTMLInputElement>) => setAmount(e.target.value)}
                    placeholder="e.g. 1.5"
                  />
                </div>
                <Button type="submit">Review Transaction</Button>
                <Button type="button" variant="outline" onClick={handleConsolidate} disabled={consolidating}>
                  {consolidating ? 'Consolidating...' : 'Consolidate Small UTXOs'}
                </Button>
              </form>
            </CardContent>
          </Card>

          <Dialog open={showConfirm} onOpenChange={(open: boolean) => { if (!open) { setShowConfirm(false); setError(null); } }}>
            <DialogContent>
              <DialogHeader>
                <DialogTitle>Confirm Transaction</DialogTitle>
              </DialogHeader>
              <div className="space-y-1">
                <DetailRow label="From">{walletName}</DetailRow>
                <DetailRow label="To" mono>{toAddress}</DetailRow>
                <DetailRow label="Amount">{amount} KGX</DetailRow>
              </div>
              {error && (
                <Alert variant="destructive">
                  <AlertDescription>{error}</AlertDescription>
                </Alert>
              )}
              <DialogFooter>
                <Button variant="outline" onClick={() => { setShowConfirm(false); setError(null); }}>Cancel</Button>
                <Button onClick={handleConfirm} disabled={loading}>
                  {loading ? 'Signing...' : 'Send'}
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>
        </div>
      )}
    </StatusGuard>
  );
}
