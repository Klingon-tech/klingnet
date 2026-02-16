import { useState } from 'react';
import { useWallet } from '../../context/WalletContext';
import { trimAmount } from '../../utils/format';
import CopyButton from '../ui/CopyButton';
import { StatusGuard } from '@/components/ui/StatusGuard';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Plus } from 'lucide-react';
import type { NewAddressResult } from '../../utils/types';

export default function Receive() {
  const { walletName, unlocked, password, accounts, balance, refreshAccounts } = useWallet();
  const [generating, setGenerating] = useState(false);
  const [newAddr, setNewAddr] = useState<NewAddressResult | null>(null);
  const [showChangeAddresses, setShowChangeAddresses] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const visibleAccounts = showChangeAddresses ? accounts : accounts.filter((a) => a.change !== 1);

  const handleGenerate = async () => {
    setGenerating(true);
    setError(null);
    setNewAddr(null);
    try {
      const mod = await import('../../../wailsjs/go/main/WalletService');
      const result = await mod.NewAddress(walletName, password);
      setNewAddr(result);
      await refreshAccounts();
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setGenerating(false);
    }
  };

  return (
    <StatusGuard walletName={walletName} unlocked={unlocked}>
      <div className="space-y-4">
        {error && (
          <Alert variant="destructive">
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        )}

        {newAddr && (
          <Alert>
            <AlertDescription>
              <div className="flex items-center gap-2">
                <span className="flex-1">
                  New address #{newAddr.index}: <span className="font-mono">{newAddr.address}</span>
                </span>
                <CopyButton text={newAddr.address} />
              </div>
            </AlertDescription>
          </Alert>
        )}

        {accounts.length > 0 ? (
          <Card>
            <CardHeader>
              <CardTitle>Addresses</CardTitle>
              <p className="text-sm text-muted-foreground">
                Balance: <span className="font-mono">{trimAmount(balance.spendable)} KGX</span>
              </p>
              <label className="inline-flex items-center gap-2 text-sm text-muted-foreground">
                <input
                  type="checkbox"
                  checked={showChangeAddresses}
                  onChange={(e) => setShowChangeAddresses(e.target.checked)}
                />
                Show change addresses
              </label>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="rounded-md border">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b bg-muted/50">
                      <th className="px-4 py-2 text-left font-medium">#</th>
                      <th className="px-4 py-2 text-left font-medium">Type</th>
                      <th className="px-4 py-2 text-left font-medium">Name</th>
                      <th className="px-4 py-2 text-left font-medium">Address</th>
                      <th className="px-4 py-2 text-right font-medium"></th>
                    </tr>
                  </thead>
                  <tbody>
                    {visibleAccounts.map((a) => (
                      <tr key={`${a.change}-${a.index}-${a.address}`} className="border-b last:border-0">
                        <td className="px-4 py-2">{a.index}</td>
                        <td className="px-4 py-2">{a.change === 1 ? 'Change' : 'Deposit'}</td>
                        <td className="px-4 py-2">{a.name}</td>
                        <td className="px-4 py-2 font-mono text-xs break-all">{a.address}</td>
                        <td className="px-4 py-2 text-right">
                          <CopyButton text={a.address} />
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              <Button onClick={handleGenerate} disabled={generating}>
                <Plus className="h-4 w-4 mr-1" />
                {generating ? 'Generating...' : 'Generate New Address'}
              </Button>
            </CardContent>
          </Card>
        ) : (
          <Card>
            <CardContent className="flex flex-col items-center justify-center py-12 text-muted-foreground">
              <p>No accounts found.</p>
            </CardContent>
          </Card>
        )}
      </div>
    </StatusGuard>
  );
}
