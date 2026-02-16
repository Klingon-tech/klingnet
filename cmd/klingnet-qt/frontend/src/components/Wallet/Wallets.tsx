import { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { useWallet } from '../../context/WalletContext';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Badge } from '@/components/ui/badge';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Plus, Import, Trash2, ArrowRightLeft, RotateCcw } from 'lucide-react';
import type { SubChainEntry } from '../../utils/types';

export default function Wallets() {
  const { walletName, wallets, unlocked, password, accounts, setActiveWallet, refreshWallets, refreshAccounts, lock } = useWallet();
  const navigate = useNavigate();

  const [deleteTarget, setDeleteTarget] = useState<string | null>(null);
  const [deleteError, setDeleteError] = useState<string | null>(null);

  const [rescanHeight, setRescanHeight] = useState('0');
  const [rescanChainID, setRescanChainID] = useState('');
  const [rescanning, setRescanning] = useState(false);
  const [rescanResult, setRescanResult] = useState<{ addressesFound: number; addressesNew: number; fromHeight: number; toHeight: number } | null>(null);
  const [rescanError, setRescanError] = useState<string | null>(null);

  const [subChains, setSubChains] = useState<SubChainEntry[]>([]);
  useEffect(() => {
    if (accounts.length === 0) return;
    (async () => {
      try {
        const mod = await import('../../../wailsjs/go/main/SubChainService');
        const result = await mod.ListSubChains(accounts.map(a => a.address));
        setSubChains(result?.chains?.filter((c: SubChainEntry) => c.syncing) || []);
      } catch {
        setSubChains([]);
      }
    })();
  }, [accounts]);

  const handleDelete = async (name: string) => {
    try {
      const mod = await import('../../../wailsjs/go/main/WalletService');
      await mod.DeleteWallet(name);
      setDeleteTarget(null);
      setDeleteError(null);
      await refreshWallets();
      if (name === walletName) {
        lock();
        await setActiveWallet('');
      }
    } catch (err: unknown) {
      setDeleteError(err instanceof Error ? err.message : String(err));
    }
  };

  const handleRescan = async () => {
    if (!walletName || !unlocked) return;
    setRescanning(true);
    setRescanResult(null);
    setRescanError(null);
    try {
      const mod = await import('../../../wailsjs/go/main/WalletService');
      const height = parseInt(rescanHeight, 10) || 0;
      const result = await mod.RescanWallet(walletName, password, height, rescanChainID);
      setRescanResult({
        addressesFound: result.addresses_found,
        addressesNew: result.addresses_new,
        fromHeight: result.from_height,
        toHeight: result.to_height,
      });
      if (result.addresses_new > 0) {
        await refreshAccounts();
      }
    } catch (err: unknown) {
      setRescanError(err instanceof Error ? err.message : String(err));
    } finally {
      setRescanning(false);
    }
  };

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader>
          <CardTitle>Your Wallets</CardTitle>
        </CardHeader>
        <CardContent>
          {wallets.length > 0 ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Wallet Name</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {wallets.map((w) => (
                  <TableRow key={w}>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        {w}
                        {w === walletName && <Badge variant="secondary">Active</Badge>}
                      </div>
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex gap-2 justify-end">
                        {w !== walletName && (
                          <Button variant="outline" size="sm" onClick={() => setActiveWallet(w)}>
                            <ArrowRightLeft className="h-3.5 w-3.5 mr-1" />
                            Switch
                          </Button>
                        )}
                        <Button variant="destructive" size="sm" onClick={() => setDeleteTarget(w)}>
                          <Trash2 className="h-3.5 w-3.5 mr-1" />
                          Delete
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <p className="text-center text-muted-foreground py-8">
              No wallets yet. Create or import one below.
            </p>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Add Wallet</CardTitle>
        </CardHeader>
        <CardContent className="flex gap-3">
          <Button onClick={() => navigate('/wallet/create?tab=create')}>
            <Plus className="h-4 w-4 mr-1.5" />
            Create New Wallet
          </Button>
          <Button variant="outline" onClick={() => navigate('/wallet/create?tab=import')}>
            <Import className="h-4 w-4 mr-1.5" />
            Import Existing Wallet
          </Button>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <RotateCcw className="h-5 w-5" />
            Rescan Wallet
          </CardTitle>
        </CardHeader>
        <CardContent>
          {!walletName ? (
            <p className="text-sm text-muted-foreground">Select a wallet first.</p>
          ) : !unlocked ? (
            <p className="text-sm text-muted-foreground">
              Unlock your wallet using the header button to enable rescan.
            </p>
          ) : (
            <div className="space-y-4">
              <p className="text-sm text-muted-foreground">
                Re-derive addresses and scan blocks to discover missing UTXOs for "{walletName}".
              </p>
              <div className="space-y-2">
                <Label>Chain</Label>
                <Select value={rescanChainID} onValueChange={setRescanChainID}>
                  <SelectTrigger>
                    <SelectValue placeholder="Root Chain (KGX)" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value=" ">Root Chain (KGX)</SelectItem>
                    {subChains.map(sc => (
                      <SelectItem key={sc.chain_id} value={sc.chain_id}>
                        {sc.name} ({sc.symbol})
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-2">
                <Label>From Block Height</Label>
                <Input
                  type="number"
                  min="0"
                  value={rescanHeight}
                  onChange={(e: React.ChangeEvent<HTMLInputElement>) => setRescanHeight(e.target.value)}
                />
              </div>
              <Button onClick={handleRescan} disabled={rescanning}>
                {rescanning ? 'Scanning...' : 'Rescan'}
              </Button>
              {rescanResult && (
                <Alert>
                  <AlertDescription>
                    Rescan complete (blocks {rescanResult.fromHeight} &rarr; {rescanResult.toHeight}).
                    Found {rescanResult.addressesFound} addresses ({rescanResult.addressesNew} new).
                  </AlertDescription>
                </Alert>
              )}
              {rescanError && (
                <Alert variant="destructive">
                  <AlertDescription>{rescanError}</AlertDescription>
                </Alert>
              )}
            </div>
          )}
        </CardContent>
      </Card>

      <Dialog open={!!deleteTarget} onOpenChange={(open: boolean) => { if (!open) { setDeleteTarget(null); setDeleteError(null); } }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete Wallet</DialogTitle>
            <DialogDescription>
              Are you sure you want to delete wallet "{deleteTarget}"? This cannot be undone unless you have your recovery phrase.
            </DialogDescription>
          </DialogHeader>
          {deleteError && (
            <Alert variant="destructive">
              <AlertDescription>{deleteError}</AlertDescription>
            </Alert>
          )}
          <DialogFooter>
            <Button variant="outline" onClick={() => { setDeleteTarget(null); setDeleteError(null); }}>
              Cancel
            </Button>
            <Button variant="destructive" onClick={() => deleteTarget && handleDelete(deleteTarget)}>
              Delete
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
