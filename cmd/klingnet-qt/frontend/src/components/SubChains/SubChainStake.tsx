import { useState, useEffect } from 'react';
import { useWallet } from '../../context/WalletContext';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
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

export default function SubChainStake() {
  const { walletName, unlocked, password, accounts, refreshAccounts } = useWallet();
  const [chains, setChains] = useState<SubChainEntry[]>([]);
  const [selectedChain, setSelectedChain] = useState('');
  const [amount, setAmount] = useState('');
  const [mode, setMode] = useState<'stake' | 'unstake'>('stake');
  const [showConfirm, setShowConfirm] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<{ tx_hash: string; pubkey: string; amount?: string } | null>(null);

  useEffect(() => {
    if (accounts.length === 0) return;
    (async () => {
      try {
        const mod = await import('../../../wailsjs/go/main/SubChainService');
        const res = await mod.ListSubChains(accounts.map(a => a.address));
        setChains(res?.chains?.filter((c: SubChainEntry) => c.syncing && c.consensus_type === 'poa') || []);
      } catch {
        setChains([]);
      }
    })();
  }, [accounts]);

  const selected = chains.find(c => c.chain_id === selectedChain);
  const symbol = selected?.symbol || 'coins';

  const handleConfirm = async () => {
    setLoading(true);
    setError(null);
    try {
      const mod = await import('../../../wailsjs/go/main/SubChainService');
      if (mode === 'stake') {
        const res = await mod.SubChainStake({
          chain_id: selectedChain,
          wallet_name: walletName,
          password: password,
          amount: amount,
        });
        setResult({ tx_hash: res.tx_hash, pubkey: res.pubkey });
      } else {
        const res = await mod.SubChainUnstake({
          chain_id: selectedChain,
          wallet_name: walletName,
          password: password,
        });
        setResult({ tx_hash: res.tx_hash, pubkey: res.pubkey, amount: res.amount });
      }
      setShowConfirm(false);
      refreshAccounts();
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  };

  const reset = () => {
    setAmount('');
    setError(null);
    setResult(null);
    setShowConfirm(false);
  };

  return (
    <StatusGuard walletName={walletName} unlocked={unlocked}>
      <div className="space-y-6">
        {result ? (
          <Card>
            <CardContent className="space-y-4">
              <Alert>
                <AlertDescription>
                  {mode === 'stake' ? 'Stake' : 'Unstake'} transaction submitted successfully!
                </AlertDescription>
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
              {result.amount && (
                <DetailRow label="Returned Amount">{result.amount} {symbol}</DetailRow>
              )}
              <div className="pt-2">
                <Button onClick={reset}>Done</Button>
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

            <Card>
              <CardHeader>
                <CardTitle>Sub-Chain Staking</CardTitle>
              </CardHeader>
              <CardContent className="space-y-4">
                <div className="flex gap-2">
                  <Button
                    size="sm"
                    variant={mode === 'stake' ? 'default' : 'outline'}
                    onClick={() => { setMode('stake'); setError(null); }}
                  >
                    Stake
                  </Button>
                  <Button
                    size="sm"
                    variant={mode === 'unstake' ? 'default' : 'outline'}
                    onClick={() => { setMode('unstake'); setError(null); }}
                  >
                    Unstake
                  </Button>
                </div>

                <div className="space-y-2">
                  <Label>Sub-Chain</Label>
                  {chains.length === 0 ? (
                    <p className="text-muted-foreground text-sm py-1">
                      No synced PoA sub-chains available. Staking is only supported on PoA chains.
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

                {mode === 'stake' && (
                  <div className="space-y-2">
                    <Label>Stake Amount ({symbol})</Label>
                    <Input
                      type="text"
                      value={amount}
                      onChange={(e: React.ChangeEvent<HTMLInputElement>) => setAmount(e.target.value)}
                      placeholder="e.g. 10"
                    />
                  </div>
                )}

                {mode === 'unstake' && (
                  <p className="text-destructive text-sm">
                    This will withdraw ALL staked {symbol} and remove you as a validator on this sub-chain.
                  </p>
                )}

                {selected && (
                  <p className="text-muted-foreground text-sm">
                    Balance on {selected.name}: {selected.balance} {selected.symbol}
                  </p>
                )}

                <Button
                  disabled={!selectedChain || chains.length === 0 || (mode === 'stake' && !amount)}
                  onClick={() => {
                    if (!selectedChain) { setError('Select a sub-chain'); return; }
                    if (mode === 'stake' && (!amount || parseFloat(amount) <= 0)) { setError('Enter a valid amount'); return; }
                    setError(null);
                    setShowConfirm(true);
                  }}
                >
                  Review {mode === 'stake' ? 'Stake' : 'Unstake'}
                </Button>
              </CardContent>
            </Card>

            <Dialog open={showConfirm} onOpenChange={(open: boolean) => { if (!open) { setShowConfirm(false); setError(null); } }}>
              <DialogContent>
                <DialogHeader>
                  <DialogTitle>Confirm Sub-Chain {mode === 'stake' ? 'Stake' : 'Unstake'}</DialogTitle>
                </DialogHeader>
                <div className="space-y-0">
                  <DetailRow label="Chain">{selected?.name} ({selected?.symbol})</DetailRow>
                  <DetailRow label="Wallet">{walletName}</DetailRow>
                  {mode === 'stake' && (
                    <DetailRow label="Amount">{amount} {symbol}</DetailRow>
                  )}
                </div>
                {mode === 'unstake' && (
                  <p className="text-muted-foreground text-xs">
                    This will withdraw all staked {symbol} and deregister as a validator.
                  </p>
                )}

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
                    {loading ? 'Signing...' : (mode === 'stake' ? 'Stake' : 'Unstake')}
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
