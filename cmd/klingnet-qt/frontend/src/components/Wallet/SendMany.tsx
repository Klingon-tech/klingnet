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
import { Plus, Trash2 } from 'lucide-react';

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

interface Recipient {
  address: string;
  amount: string;
}

export default function SendMany() {
  const { walletName, unlocked, password, refreshAccounts } = useWallet();
  const [recipients, setRecipients] = useState<Recipient[]>([{ address: '', amount: '' }]);
  const [showConfirm, setShowConfirm] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [txHash, setTxHash] = useState<string | null>(null);

  const addRecipient = () => {
    setRecipients([...recipients, { address: '', amount: '' }]);
  };

  const removeRecipient = (index: number) => {
    if (recipients.length <= 1) return;
    setRecipients(recipients.filter((_, i) => i !== index));
  };

  const updateRecipient = (index: number, field: keyof Recipient, value: string) => {
    const updated = [...recipients];
    updated[index] = { ...updated[index], [field]: value };
    setRecipients(updated);
  };

  const totalAmount = recipients.reduce((sum, r) => {
    const n = parseFloat(r.amount);
    return sum + (isNaN(n) ? 0 : n);
  }, 0);

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    for (let i = 0; i < recipients.length; i++) {
      const addrErr = validateAddress(recipients[i].address);
      if (addrErr) { setError(`Recipient ${i + 1}: ${addrErr}`); return; }
      const amtErr = validateAmount(recipients[i].amount);
      if (amtErr) { setError(`Recipient ${i + 1}: ${amtErr}`); return; }
    }
    setError(null);
    setShowConfirm(true);
  };

  const handleConfirm = async () => {
    setLoading(true);
    setError(null);
    try {
      const mod = await import('../../../wailsjs/go/main/WalletService');
      const models = await import('../../../wailsjs/go/models');
      const req = new models.main.SendManyRequest({
        wallet_name: walletName,
        password: password,
        recipients: recipients.map(r => new models.main.SendManyRecipient({
          to_address: r.address,
          amount: r.amount,
        })),
      });
      const result = await mod.SendManyTransaction(req);
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
    setRecipients([{ address: '', amount: '' }]);
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
            <DetailRow label="Recipients">{recipients.length}</DetailRow>
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

          <Card>
            <CardContent className="pt-6">
              <form onSubmit={handleSubmit} className="space-y-4">
                {recipients.map((r, i) => (
                  <Card key={i}>
                    <CardContent className="pt-4 space-y-4">
                      <div className="flex items-center justify-between">
                        <span className="text-sm font-semibold">Recipient {i + 1}</span>
                        {recipients.length > 1 && (
                          <Button
                            type="button"
                            variant="destructive"
                            size="sm"
                            onClick={() => removeRecipient(i)}
                          >
                            <Trash2 className="h-3.5 w-3.5 mr-1" />
                            Remove
                          </Button>
                        )}
                      </div>
                      <div className="space-y-2">
                        <Label>Address</Label>
                        <Input
                          value={r.address}
                          onChange={(e: React.ChangeEvent<HTMLInputElement>) => updateRecipient(i, 'address', e.target.value)}
                          placeholder="kgx1..."
                          className="font-mono"
                        />
                      </div>
                      <div className="space-y-2">
                        <Label>Amount (KGX)</Label>
                        <Input
                          value={r.amount}
                          onChange={(e: React.ChangeEvent<HTMLInputElement>) => updateRecipient(i, 'amount', e.target.value)}
                          placeholder="e.g. 1.5"
                        />
                      </div>
                    </CardContent>
                  </Card>
                ))}

                <Button type="button" variant="outline" onClick={addRecipient}>
                  <Plus className="h-4 w-4 mr-1" />
                  Add Recipient
                </Button>

                <p className="text-sm font-semibold">
                  Total: {totalAmount.toFixed(12)} KGX ({recipients.length} recipient{recipients.length !== 1 ? 's' : ''})
                </p>

                <Button type="submit">Review Transaction</Button>
              </form>
            </CardContent>
          </Card>

          <Dialog open={showConfirm} onOpenChange={(open: boolean) => { if (!open) { setShowConfirm(false); setError(null); } }}>
            <DialogContent>
              <DialogHeader>
                <DialogTitle>Confirm Send Many</DialogTitle>
              </DialogHeader>
              <div className="space-y-1">
                <DetailRow label="From">{walletName}</DetailRow>
                {recipients.map((r, i) => (
                  <div key={i} className="border-b border-border pb-2 mb-2 last:border-0 last:pb-0 last:mb-0">
                    <DetailRow label={`To (${i + 1})`} mono>
                      <span className="text-xs">{r.address}</span>
                    </DetailRow>
                    <DetailRow label="Amount">{r.amount} KGX</DetailRow>
                  </div>
                ))}
                <DetailRow label="Total">
                  <span className="font-semibold">{totalAmount.toFixed(12)} KGX</span>
                </DetailRow>
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
