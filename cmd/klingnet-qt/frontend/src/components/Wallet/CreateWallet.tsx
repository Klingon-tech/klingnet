import { useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { useWallet } from '../../context/WalletContext';
import CopyButton from '../ui/CopyButton';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Textarea } from '@/components/ui/textarea';
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { DetailRow } from '@/components/ui/DetailRow';
import { AlertTriangle } from 'lucide-react';

type Tab = 'create' | 'import';
type Step = 'mnemonic' | 'password' | 'done';

export default function CreateWallet() {
  const { refreshWallets, setActiveWallet } = useWallet();
  const [searchParams] = useSearchParams();
  const initialTab = searchParams.get('tab') === 'import' ? 'import' : 'create';
  const [tab, setTab] = useState<Tab>(initialTab);
  const [step, setStep] = useState<Step>('mnemonic');
  const [name, setName] = useState('');
  const [mnemonic, setMnemonic] = useState('');
  const [password, setPassword] = useState('');
  const [confirmPw, setConfirmPw] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [result, setResult] = useState<{ name: string; address: string } | null>(null);

  const handleGenerate = async () => {
    try {
      const mod = await import('../../../wailsjs/go/main/WalletService');
      const m = await mod.GenerateMnemonic();
      setMnemonic(m);
      setStep('mnemonic');
      setError(null);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const handleMnemonicNext = async () => {
    if (!name.trim()) {
      setError('Wallet name is required');
      return;
    }
    if (tab === 'import' && !mnemonic.trim()) {
      setError('Mnemonic is required');
      return;
    }
    if (tab === 'create' && !mnemonic) {
      await handleGenerate();
      if (!mnemonic) return;
    }
    setError(null);
    setStep('password');
  };

  const handleCreate = async () => {
    if (password.length < 8) {
      setError('Password must be at least 8 characters');
      return;
    }
    if (password !== confirmPw) {
      setError('Passwords do not match');
      return;
    }

    setLoading(true);
    setError(null);
    try {
      const mod = await import('../../../wailsjs/go/main/WalletService');
      const fn = tab === 'create' ? mod.CreateWallet : mod.ImportWallet;
      const cleaned = mnemonic.trim().replace(/\s+/g, ' ');
      const info = await fn(name, password, cleaned);
      setResult(info);
      setStep('done');
      setMnemonic('');
      await refreshWallets();
      await setActiveWallet(info.name);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  };

  const reset = () => {
    setStep('mnemonic');
    setName('');
    setMnemonic('');
    setPassword('');
    setConfirmPw('');
    setError(null);
    setResult(null);
  };

  return (
    <div className="space-y-6">
      <Tabs value={tab} onValueChange={(v: string) => { setTab(v as Tab); reset(); }}>
        <TabsList>
          <TabsTrigger value="create">Create New</TabsTrigger>
          <TabsTrigger value="import">Import Existing</TabsTrigger>
        </TabsList>
      </Tabs>

      {/* Progress indicator */}
      <div className="flex gap-2">
        {['mnemonic', 'password', 'done'].map((s, i) => (
          <div
            key={s}
            className={`h-1.5 flex-1 rounded-full transition-colors ${
              s === step ? 'bg-primary' :
              i < ['mnemonic', 'password', 'done'].indexOf(step) ? 'bg-primary/50' : 'bg-muted'
            }`}
          />
        ))}
      </div>

      {error && (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}

      {step === 'mnemonic' && (
        <Card>
          <CardContent className="pt-6 space-y-4">
            <div className="space-y-2">
              <Label>Wallet Name</Label>
              <Input
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="e.g. My Wallet"
              />
            </div>

            {tab === 'create' && (
              <>
                {!mnemonic ? (
                  <Button onClick={handleGenerate}>Generate Mnemonic</Button>
                ) : (
                  <div className="space-y-3">
                    <h3 className="text-sm font-semibold">Recovery Phrase</h3>
                    <Alert>
                      <AlertTriangle className="h-4 w-4" />
                      <AlertDescription>
                        Write these words down and store them safely. Anyone with this phrase can access your funds.
                      </AlertDescription>
                    </Alert>
                    <div className="grid grid-cols-4 gap-2">
                      {mnemonic.split(' ').map((word, i) => (
                        <div key={i} className="flex items-center gap-2 px-3 py-2 bg-muted rounded-md text-sm">
                          <span className="text-muted-foreground text-xs w-5">{i + 1}.</span>
                          <span className="font-mono font-medium">{word}</span>
                        </div>
                      ))}
                    </div>
                  </div>
                )}
              </>
            )}

            {tab === 'import' && (
              <div className="space-y-2">
                <Label>Recovery Phrase (24 words)</Label>
                <Textarea
                  value={mnemonic}
                  onChange={(e) => setMnemonic(e.target.value)}
                  placeholder="Enter your 24-word recovery phrase..."
                  rows={3}
                  className="font-mono"
                />
              </div>
            )}

            {(mnemonic || tab === 'import') && (
              <Button onClick={handleMnemonicNext}>Continue</Button>
            )}
          </CardContent>
        </Card>
      )}

      {step === 'password' && (
        <Card>
          <CardHeader>
            <CardTitle>Set Password</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <p className="text-sm text-muted-foreground">
              This password encrypts your wallet on this device. You'll need it to send transactions.
            </p>
            <div className="space-y-2">
              <Label>Password (min 8 characters)</Label>
              <Input
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder="Enter password"
              />
            </div>
            <div className="space-y-2">
              <Label>Confirm Password</Label>
              <Input
                type="password"
                value={confirmPw}
                onChange={(e) => setConfirmPw(e.target.value)}
                placeholder="Confirm password"
              />
            </div>
            <div className="flex gap-2">
              <Button variant="outline" onClick={() => setStep('mnemonic')}>Back</Button>
              <Button onClick={handleCreate} disabled={loading}>
                {loading ? 'Encrypting...' : tab === 'create' ? 'Create Wallet' : 'Import Wallet'}
              </Button>
            </div>
          </CardContent>
        </Card>
      )}

      {step === 'done' && result && (
        <Card>
          <CardContent className="pt-6 space-y-4">
            <Alert>
              <AlertDescription>Wallet "{result.name}" created successfully!</AlertDescription>
            </Alert>
            <DetailRow label="Name">{result.name}</DetailRow>
            <DetailRow label="Address" mono>
              <div className="flex items-center gap-1">
                <span className="break-all">{result.address}</span>
                <CopyButton text={result.address} />
              </div>
            </DetailRow>
            <Button onClick={reset}>Create Another</Button>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
