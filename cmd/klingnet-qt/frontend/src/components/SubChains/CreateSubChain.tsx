import { useState } from 'react';
import { useWallet } from '../../context/WalletContext';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Textarea } from '@/components/ui/textarea';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { StatusGuard } from '@/components/ui/StatusGuard';
import { DetailRow } from '@/components/ui/DetailRow';
import CopyButton from '../ui/CopyButton';

export default function CreateSubChain() {
  const { walletName, unlocked, password, refreshAccounts } = useWallet();
  const [chainName, setChainName] = useState('');
  const [symbol, setSymbol] = useState('');
  const [consensusType, setConsensusType] = useState('poa');
  const [blockTime, setBlockTime] = useState('5');
  const [blockReward, setBlockReward] = useState('0.001');
  const [maxSupply, setMaxSupply] = useState('1000000');
  const [minFeeRate, setMinFeeRate] = useState('10');
  const [validators, setValidators] = useState('');
  const [difficulty, setDifficulty] = useState('1000');
  const [difficultyAdjust, setDifficultyAdjust] = useState('0');
  const [showConfirm, setShowConfirm] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<{ tx_hash: string; chain_id: string } | null>(null);

  const displaySymbol = symbol || '???';

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!chainName) { setError('Enter chain name'); return; }
    if (!symbol) { setError('Enter chain symbol'); return; }
    if (!/^[A-Z0-9]{2,10}$/.test(symbol)) { setError('Symbol must be 2-10 uppercase letters/digits'); return; }
    if (consensusType === 'poa' && !validators.trim()) { setError('PoA requires at least one validator public key'); return; }
    setError(null);
    setShowConfirm(true);
  };

  const handleConfirm = async () => {
    setLoading(true);
    setError(null);
    try {
      const mod = await import('../../../wailsjs/go/main/SubChainService');
      const validatorList = consensusType === 'poa'
        ? validators.split('\n').map(v => v.trim()).filter(v => v.length > 0)
        : undefined;
      const res = await mod.CreateSubChain({
        wallet_name: walletName,
        password: password,
        chain_name: chainName,
        symbol: symbol,
        consensus_type: consensusType,
        block_time: parseInt(blockTime) || 5,
        block_reward: blockReward,
        max_supply: maxSupply,
        min_fee_rate: minFeeRate,
        validators: validatorList,
        initial_difficulty: consensusType === 'pow' ? parseInt(difficulty) || 1000 : undefined,
        difficulty_adjust: consensusType === 'pow' ? parseInt(difficultyAdjust) || 0 : undefined,
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
    setChainName('');
    setSymbol('');
    setConsensusType('poa');
    setBlockTime('5');
    setBlockReward('0.001');
    setMaxSupply('1000000');
    setMinFeeRate('10');
    setValidators('');
    setDifficulty('1000');
    setDifficultyAdjust('0');
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
                <AlertDescription>Sub-chain created successfully!</AlertDescription>
              </Alert>
              <DetailRow label="Tx Hash" mono>
                <div className="flex items-center gap-1">
                  <span className="break-all">{result.tx_hash}</span>
                  <CopyButton text={result.tx_hash} />
                </div>
              </DetailRow>
              <DetailRow label="Chain ID" mono>
                <div className="flex items-center gap-1">
                  <span className="break-all">{result.chain_id}</span>
                  <CopyButton text={result.chain_id} />
                </div>
              </DetailRow>
              <div className="pt-2">
                <Button onClick={reset}>Create Another</Button>
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
                <CardTitle>Create Sub-Chain</CardTitle>
              </CardHeader>
              <CardContent className="space-y-4">
                <p className="text-muted-foreground text-sm">
                  Launch a new sub-chain on Klingnet. The registration fee (1,000 KGX on mainnet) is burned automatically.
                </p>
                <form onSubmit={handleSubmit} className="space-y-4">
                  <div className="space-y-2">
                    <Label>Chain Name</Label>
                    <Input type="text" value={chainName} onChange={(e: React.ChangeEvent<HTMLInputElement>) => setChainName(e.target.value)} placeholder="e.g. MyChain" />
                  </div>
                  <div className="space-y-2">
                    <Label>Symbol</Label>
                    <Input
                      type="text"
                      value={symbol}
                      onChange={(e: React.ChangeEvent<HTMLInputElement>) => setSymbol(e.target.value.toUpperCase().replace(/[^A-Z0-9]/g, ''))}
                      placeholder="e.g. MYC"
                      maxLength={10}
                      className="uppercase"
                    />
                  </div>
                  <div className="space-y-2">
                    <Label>Consensus Type</Label>
                    <Select value={consensusType} onValueChange={setConsensusType}>
                      <SelectTrigger>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="poa">Proof of Authority (PoA)</SelectItem>
                        <SelectItem value="pow">Proof of Work (PoW)</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>
                  <div className="space-y-2">
                    <Label>Block Time (seconds)</Label>
                    <Input type="number" value={blockTime} onChange={(e: React.ChangeEvent<HTMLInputElement>) => setBlockTime(e.target.value)} min="1" />
                  </div>
                  <div className="space-y-2">
                    <Label>Block Reward ({displaySymbol})</Label>
                    <Input type="text" value={blockReward} onChange={(e: React.ChangeEvent<HTMLInputElement>) => setBlockReward(e.target.value)} placeholder="0.001" />
                  </div>
                  <div className="space-y-2">
                    <Label>Max Supply ({displaySymbol})</Label>
                    <Input type="text" value={maxSupply} onChange={(e: React.ChangeEvent<HTMLInputElement>) => setMaxSupply(e.target.value)} placeholder="1000000" />
                  </div>
                  <div className="space-y-2">
                    <Label>Min Fee Rate (per byte)</Label>
                    <Input type="text" value={minFeeRate} onChange={(e: React.ChangeEvent<HTMLInputElement>) => setMinFeeRate(e.target.value)} placeholder="10" />
                  </div>

                  {consensusType === 'poa' && (
                    <div className="space-y-2">
                      <Label>Validator Public Keys (one per line)</Label>
                      <Textarea
                        value={validators}
                        onChange={(e: React.ChangeEvent<HTMLTextAreaElement>) => setValidators(e.target.value)}
                        placeholder="Enter 33-byte hex public keys, one per line"
                        rows={4}
                        className="font-mono"
                      />
                    </div>
                  )}

                  {consensusType === 'pow' && (
                    <>
                      <div className="space-y-2">
                        <Label>Initial Difficulty</Label>
                        <Input type="number" value={difficulty} onChange={(e: React.ChangeEvent<HTMLInputElement>) => setDifficulty(e.target.value)} min="1" />
                      </div>
                      <div className="space-y-2">
                        <Label>Difficulty Adjustment Interval (blocks, 0 = disabled, min 10)</Label>
                        <Input type="number" value={difficultyAdjust} onChange={(e: React.ChangeEvent<HTMLInputElement>) => setDifficultyAdjust(e.target.value)} min="0" />
                      </div>
                    </>
                  )}

                  <Button type="submit">Review Sub-Chain</Button>
                </form>
              </CardContent>
            </Card>

            <Dialog open={showConfirm} onOpenChange={(open: boolean) => { if (!open) { setShowConfirm(false); setError(null); } }}>
              <DialogContent>
                <DialogHeader>
                  <DialogTitle>Confirm Sub-Chain Creation</DialogTitle>
                </DialogHeader>
                <div className="space-y-0">
                  <DetailRow label="Chain Name">{chainName}</DetailRow>
                  <DetailRow label="Symbol">{symbol}</DetailRow>
                  <DetailRow label="Consensus">
                    <span className="uppercase">{consensusType}</span>
                  </DetailRow>
                  <DetailRow label="Block Time">{blockTime}s</DetailRow>
                  <DetailRow label="Block Reward">{blockReward} {symbol}</DetailRow>
                  <DetailRow label="Max Supply">{maxSupply} {symbol}</DetailRow>
                  <DetailRow label="Min Fee Rate">{minFeeRate} per byte</DetailRow>
                  {consensusType === 'pow' && (
                    <DetailRow label="Difficulty Adjust">
                      {difficultyAdjust === '0' ? 'Disabled' : `Every ${difficultyAdjust} blocks`}
                    </DetailRow>
                  )}
                  <DetailRow label="Registration Fee">Protocol fixed (burned in KGX)</DetailRow>
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
                    {loading ? 'Signing...' : 'Create Sub-Chain'}
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
