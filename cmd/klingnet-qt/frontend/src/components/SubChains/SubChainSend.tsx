import { useState, useEffect } from 'react';
import { useWallet } from '../../context/WalletContext';
import { Card, CardContent } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { StatusGuard } from '@/components/ui/StatusGuard';
import { DetailRow } from '@/components/ui/DetailRow';
import CopyButton from '../ui/CopyButton';
import type { SubChainEntry } from '../../utils/types';

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

export default function SubChainSend() {
  const { walletName, unlocked, password, accounts, refreshAccounts } = useWallet();
  const [chains, setChains] = useState<SubChainEntry[]>([]);
  const [selectedChain, setSelectedChain] = useState('');
  const [toAddress, setToAddress] = useState('');
  const [amount, setAmount] = useState('');
  const [showConfirm, setShowConfirm] = useState(false);
  const [loading, setLoading] = useState(false);
  const [consolidating, setConsolidating] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [txHash, setTxHash] = useState<string | null>(null);
  const [consolidateNote, setConsolidateNote] = useState<string | null>(null);

  useEffect(() => {
    if (accounts.length === 0) return;
    (async () => {
      try {
        const mod = await import('../../../wailsjs/go/main/SubChainService');
        const result = await mod.ListSubChains(accounts.map(a => a.address));
        if (result?.chains) {
          setChains(result.chains.filter((c: SubChainEntry) => c.syncing));
        }
      } catch {
        setChains([]);
      }
    })();
  }, [accounts]);

  const selected = chains.find(c => c.chain_id === selectedChain);
  const symbol = selected?.symbol || 'coins';

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!selectedChain) { setError('Select a sub-chain'); return; }
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
      const mod = await import('../../../wailsjs/go/main/SubChainService');
      const result = await mod.SubChainSend({
        chain_id: selectedChain,
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
    if (!selectedChain) {
      setError('Select a sub-chain');
      return;
    }
    setConsolidating(true);
    setError(null);
    setConsolidateNote(null);
    try {
      const mod = await import('../../../wailsjs/go/main/SubChainService');
      const result = await mod.SubChainConsolidate(selectedChain, walletName, password, 500);
      setConsolidateNote(
        `Consolidation submitted: ${result.inputs_used} inputs -> ${result.output_amount} ${symbol} (fee ${result.fee}). Tx: ${result.tx_hash}`,
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
      <div className="space-y-6">
        {txHash ? (
          <Card>
            <CardContent className="space-y-4">
              <Alert>
                <AlertDescription>Transaction submitted successfully!</AlertDescription>
              </Alert>
              <DetailRow label="Tx Hash" mono>
                <div className="flex items-center gap-1">
                  <span className="break-all">{txHash}</span>
                  <CopyButton text={txHash} />
                </div>
              </DetailRow>
              <div className="pt-2">
                <Button onClick={reset}>Send Another</Button>
              </div>
            </CardContent>
          </Card>
        ) : (
          <>
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
              <CardContent>
                <form onSubmit={handleSubmit} className="space-y-4">
                  <div className="space-y-2">
                    <Label>Sub-Chain</Label>
                    {chains.length === 0 ? (
                      <p className="text-muted-foreground text-sm py-1">
                        No synced sub-chains available.
                      </p>
                    ) : (
                      <Select
                        value={selectedChain || ' '}
                        onValueChange={(v: string) => setSelectedChain(v === ' ' ? '' : v)}
                      >
                        <SelectTrigger>
                          <SelectValue placeholder="Select a sub-chain..." />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value=" ">Select a sub-chain...</SelectItem>
                          {chains.map(c => (
                            <SelectItem key={c.chain_id} value={c.chain_id}>
                              {c.name} ({c.symbol})
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    )}
                  </div>

                  <div className="space-y-2">
                    <Label>Recipient Address</Label>
                    <Input
                      type="text"
                      value={toAddress}
                      onChange={(e: React.ChangeEvent<HTMLInputElement>) => setToAddress(e.target.value)}
                      placeholder="kgx1..."
                      className="font-mono"
                    />
                  </div>

                  <div className="space-y-2">
                    <Label>Amount ({symbol})</Label>
                    <Input
                      type="text"
                      value={amount}
                      onChange={(e: React.ChangeEvent<HTMLInputElement>) => setAmount(e.target.value)}
                      placeholder="e.g. 1.5"
                    />
                  </div>

                  {selected && (
                    <p className="text-muted-foreground text-sm">
                      Balance on {selected.name}: {selected.balance} {selected.symbol}
                    </p>
                  )}

                  <Button type="submit" disabled={chains.length === 0}>
                    Review Transaction
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    onClick={handleConsolidate}
                    disabled={chains.length === 0 || consolidating}
                  >
                    {consolidating ? 'Consolidating...' : 'Consolidate Small UTXOs'}
                  </Button>
                </form>
              </CardContent>
            </Card>

            <Dialog open={showConfirm} onOpenChange={(open: boolean) => { if (!open) { setShowConfirm(false); setError(null); } }}>
              <DialogContent>
                <DialogHeader>
                  <DialogTitle>Confirm Sub-Chain Transaction</DialogTitle>
                </DialogHeader>
                <div className="space-y-0">
                  <DetailRow label="Chain">{selected?.name} ({selected?.symbol})</DetailRow>
                  <DetailRow label="From">{walletName}</DetailRow>
                  <DetailRow label="To" mono>{toAddress}</DetailRow>
                  <DetailRow label="Amount">{amount} {symbol}</DetailRow>
                </div>

                {error && (
                  <Alert variant="destructive">
                    <AlertDescription>{error}</AlertDescription>
                  </Alert>
                )}

                <DialogFooter>
                  <Button variant="outline" onClick={() => { setShowConfirm(false); setError(null); }}>
                    Cancel
                  </Button>
                  <Button onClick={handleConfirm} disabled={loading}>
                    {loading ? 'Signing...' : 'Send'}
                  </Button>
                </DialogFooter>
              </DialogContent>
            </Dialog>
          </>
        )}
      </div>
    </StatusGuard>
  );
}
