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
import { AlertTriangle } from 'lucide-react';

export default function ExportKey() {
  const { walletName, unlocked, password } = useWallet();
  const [account, setAccount] = useState('0');
  const [index, setIndex] = useState('0');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<{ private_key: string; pubkey: string; address: string } | null>(null);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setLoading(true);
    setError(null);
    try {
      const mod = await import('../../../wailsjs/go/main/WalletService');
      const res = await mod.ExportValidatorKey(
        walletName,
        password,
        parseInt(account) || 0,
        parseInt(index) || 0,
      );
      setResult(res);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  };

  const reset = () => {
    setError(null);
    setResult(null);
  };

  return (
    <StatusGuard walletName={walletName} unlocked={unlocked}>
      {result ? (
        <Card>
          <CardContent className="pt-6 space-y-4">
            <Alert>
              <AlertDescription>Key exported successfully. Keep this private key secure!</AlertDescription>
            </Alert>
            <DetailRow label="Private Key" mono>
              <div className="flex items-center gap-1">
                <span className="break-all">{result.private_key}</span>
                <CopyButton text={result.private_key} />
              </div>
            </DetailRow>
            <DetailRow label="Public Key" mono>
              <div className="flex items-center gap-1">
                <span className="break-all">{result.pubkey}</span>
                <CopyButton text={result.pubkey} />
              </div>
            </DetailRow>
            <DetailRow label="Address" mono>
              <div className="flex items-center gap-1">
                <span className="break-all">{result.address}</span>
                <CopyButton text={result.address} />
              </div>
            </DetailRow>
            <Button variant="outline" onClick={reset}>Close</Button>
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
              <CardTitle>Export Validator Key</CardTitle>
              <p className="text-sm text-muted-foreground">
                Export a private key for use as a validator key with klingnetd --validator-key.
                The key is derived at BIP-32 path m/44'/8888'/account'/0/index.
              </p>
            </CardHeader>
            <CardContent className="space-y-4">
              <Alert>
                <AlertTriangle className="h-4 w-4" />
                <AlertDescription>
                  Never share your private key. Anyone with access to this key can sign blocks and control validator operations.
                </AlertDescription>
              </Alert>
              <form onSubmit={handleSubmit} className="space-y-4">
                <div className="space-y-2">
                  <Label>Account Index</Label>
                  <Input
                    type="number"
                    value={account}
                    onChange={(e: React.ChangeEvent<HTMLInputElement>) => setAccount(e.target.value)}
                    min={0}
                  />
                </div>
                <div className="space-y-2">
                  <Label>Key Index</Label>
                  <Input
                    type="number"
                    value={index}
                    onChange={(e: React.ChangeEvent<HTMLInputElement>) => setIndex(e.target.value)}
                    min={0}
                  />
                </div>
                <Button type="submit" disabled={loading}>
                  {loading ? 'Deriving...' : 'Export Key'}
                </Button>
              </form>
            </CardContent>
          </Card>
        </div>
      )}
    </StatusGuard>
  );
}
