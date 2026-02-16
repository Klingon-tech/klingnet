import { useState } from 'react';
import { useWallet } from '../../context/WalletContext';
import { Card, CardContent } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { StatusGuard } from '@/components/ui/StatusGuard';
import { DetailRow } from '@/components/ui/DetailRow';
import CopyButton from '../ui/CopyButton';

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

function validateTokenID(id: string): string | null {
  if (!id) return 'Enter token ID';
  if (!/^[0-9a-fA-F]{64}$/.test(id)) {
    return 'Invalid token ID. Expected 64 hex characters';
  }
  return null;
}

function validateAmount(s: string): string | null {
  if (!s) return 'Enter amount';
  const n = parseInt(s, 10);
  if (isNaN(n) || n <= 0) return 'Amount must be a positive integer';
  return null;
}

export default function SendToken() {
  const { walletName, unlocked, password, refreshAccounts } = useWallet();
  const [tokenID, setTokenID] = useState('');
  const [toAddress, setToAddress] = useState('');
  const [amount, setAmount] = useState('');
  const [showConfirm, setShowConfirm] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [txHash, setTxHash] = useState<string | null>(null);

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const idErr = validateTokenID(tokenID);
    if (idErr) { setError(idErr); return; }
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
      const result = await mod.SendToken({
        wallet_name: walletName,
        password: password,
        token_id: tokenID,
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

  const reset = () => {
    setTokenID('');
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
            <AlertDescription>Token transfer submitted successfully!</AlertDescription>
          </Alert>
          <DetailRow label="Tx Hash">
            <div className="flex items-center break-all font-mono">
              <span className="flex-1">{txHash}</span>
              <CopyButton text={txHash} />
            </div>
          </DetailRow>
          <div className="pt-2">
            <Button onClick={reset}>Send More</Button>
          </div>
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
        <CardContent className="pt-6">
          <form onSubmit={handleSubmit} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="tokenId">Token ID</Label>
              <Input
                id="tokenId"
                type="text"
                value={tokenID}
                onChange={(e) => setTokenID(e.target.value)}
                placeholder="64 hex characters"
                className="font-mono"
              />
            </div>

            <div className="space-y-2">
              <Label htmlFor="toAddress">Recipient Address</Label>
              <Input
                id="toAddress"
                type="text"
                value={toAddress}
                onChange={(e) => setToAddress(e.target.value)}
                placeholder="kgx1..."
                className="font-mono"
              />
            </div>

            <div className="space-y-2">
              <Label htmlFor="amount">Amount (token units)</Label>
              <Input
                id="amount"
                type="text"
                value={amount}
                onChange={(e) => setAmount(e.target.value)}
                placeholder="e.g. 1000"
              />
            </div>

            <Button type="submit">Review Transfer</Button>
          </form>
        </CardContent>
      </Card>

      <Dialog open={showConfirm} onOpenChange={(open: boolean) => { if (!open) { setShowConfirm(false); setError(null); } }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Confirm Token Transfer</DialogTitle>
          </DialogHeader>
          <div className="space-y-2">
            <DetailRow label="Token ID">
              <span className="font-mono text-xs">{tokenID}</span>
            </DetailRow>
            <DetailRow label="To">{toAddress}</DetailRow>
            <DetailRow label="Amount">{amount} tokens</DetailRow>
          </div>

          {error && (
            <Alert variant="destructive">
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          )}

          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => { setShowConfirm(false); setError(null); }}
            >
              Cancel
            </Button>
            <Button onClick={handleConfirm} disabled={loading}>
              {loading ? 'Signing...' : 'Send Tokens'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      </div>
      )}
    </StatusGuard>
  );
}
