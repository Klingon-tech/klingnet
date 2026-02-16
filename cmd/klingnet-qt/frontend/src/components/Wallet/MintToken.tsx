import { useState } from 'react';
import { useWallet } from '../../context/WalletContext';
import CopyButton from '../ui/CopyButton';
import { StatusGuard } from '@/components/ui/StatusGuard';
import { DetailRow } from '@/components/ui/DetailRow';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
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

export default function MintToken() {
  const { walletName, unlocked, password, refreshAccounts } = useWallet();
  const [tokenName, setTokenName] = useState('');
  const [symbol, setSymbol] = useState('');
  const [decimals, setDecimals] = useState('8');
  const [amount, setAmount] = useState('');
  const [recipient, setRecipient] = useState('');
  const [showConfirm, setShowConfirm] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<{ tx_hash: string; token_id: string } | null>(null);

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!tokenName) { setError('Enter token name'); return; }
    if (!symbol) { setError('Enter token symbol'); return; }
    if (!amount) { setError('Enter token amount'); return; }
    setError(null);
    setShowConfirm(true);
  };

  const handleConfirm = async () => {
    setLoading(true);
    setError(null);
    try {
      const mod = await import('../../../wailsjs/go/main/WalletService');
      const res = await mod.MintToken({
        wallet_name: walletName,
        password: password,
        token_name: tokenName,
        symbol: symbol,
        decimals: parseInt(decimals) || 8,
        amount: amount,
        recipient: recipient,
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
    setTokenName('');
    setSymbol('');
    setDecimals('8');
    setAmount('');
    setRecipient('');
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
              <AlertDescription>Token minted successfully!</AlertDescription>
            </Alert>
            <DetailRow label="Tx Hash" mono>
              <div className="flex items-center gap-1">
                <span className="break-all">{result.tx_hash}</span>
                <CopyButton text={result.tx_hash} />
              </div>
            </DetailRow>
            <DetailRow label="Token ID" mono>
              <div className="flex items-center gap-1">
                <span className="break-all">{result.token_id}</span>
                <CopyButton text={result.token_id} />
              </div>
            </DetailRow>
            <Button onClick={reset}>Mint Another</Button>
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
              <CardTitle>Mint New Token</CardTitle>
              <p className="text-sm text-muted-foreground">
                Create a new token on the Klingnet chain. Requires a 50 KGX creation fee.
              </p>
            </CardHeader>
            <CardContent>
              <form onSubmit={handleSubmit} className="space-y-4">
                <div className="space-y-2">
                  <Label>Token Name</Label>
                  <Input
                    value={tokenName}
                    onChange={(e: React.ChangeEvent<HTMLInputElement>) => setTokenName(e.target.value)}
                    placeholder="e.g. My Token"
                  />
                </div>
                <div className="space-y-2">
                  <Label>Token Symbol</Label>
                  <Input
                    value={symbol}
                    onChange={(e: React.ChangeEvent<HTMLInputElement>) => setSymbol(e.target.value)}
                    placeholder="e.g. MTK"
                  />
                </div>
                <div className="space-y-2">
                  <Label>Decimals</Label>
                  <Input
                    type="number"
                    value={decimals}
                    onChange={(e: React.ChangeEvent<HTMLInputElement>) => setDecimals(e.target.value)}
                    min={0}
                    max={18}
                  />
                </div>
                <div className="space-y-2">
                  <Label>Amount</Label>
                  <Input
                    value={amount}
                    onChange={(e: React.ChangeEvent<HTMLInputElement>) => setAmount(e.target.value)}
                    placeholder="e.g. 1000000"
                  />
                </div>
                <div className="space-y-2">
                  <Label>Recipient Address (optional, defaults to sender)</Label>
                  <Input
                    value={recipient}
                    onChange={(e: React.ChangeEvent<HTMLInputElement>) => setRecipient(e.target.value)}
                    placeholder="kgx1... (leave empty for self)"
                    className="font-mono"
                  />
                </div>
                <Button type="submit">Review Mint</Button>
              </form>
            </CardContent>
          </Card>

          <Dialog open={showConfirm} onOpenChange={(open: boolean) => { if (!open) { setShowConfirm(false); setError(null); } }}>
            <DialogContent>
              <DialogHeader>
                <DialogTitle>Confirm Token Mint</DialogTitle>
              </DialogHeader>
              <div className="space-y-1">
                <DetailRow label="Token">{tokenName} ({symbol})</DetailRow>
                <DetailRow label="Amount">{amount}</DetailRow>
                <DetailRow label="Fee">50 KGX</DetailRow>
              </div>
              {error && (
                <Alert variant="destructive">
                  <AlertDescription>{error}</AlertDescription>
                </Alert>
              )}
              <DialogFooter>
                <Button variant="outline" onClick={() => { setShowConfirm(false); setError(null); }}>Cancel</Button>
                <Button onClick={handleConfirm} disabled={loading}>
                  {loading ? 'Signing...' : 'Mint Token'}
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>
        </div>
      )}
    </StatusGuard>
  );
}
