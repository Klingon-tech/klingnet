import { useState, useEffect } from 'react';
import { useChainInfo } from '../../hooks/useChain';
import { useWallet } from '../../context/WalletContext';
import { trimAmount } from '../../utils/format';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { ThemeToggle } from '@/components/ui/ThemeToggle';
import { Lock, Unlock } from 'lucide-react';
import { cn } from '@/lib/utils';

interface HeaderProps {
  title: string;
}

export default function Header({ title }: HeaderProps) {
  const { data: chain, error } = useChainInfo();
  const { walletName, unlocked, accounts, balance, unlock, lock } = useWallet();
  const [showUnlock, setShowUnlock] = useState(false);
  const [pw, setPw] = useState('');
  const [unlockError, setUnlockError] = useState('');
  const [startupErr, setStartupErr] = useState('');

  useEffect(() => {
    (async () => {
      try {
        const mod = await import('../../../wailsjs/go/main/App');
        const err = await mod.GetStartupError();
        if (err) setStartupErr(err);
      } catch {
        // ignore
      }
    })();
  }, []);

  const statusClass = startupErr ? 'bg-destructive' : error ? 'bg-destructive' : chain ? 'bg-primary' : 'bg-yellow-500';
  const statusText = startupErr ? 'Node Error' : error ? 'Disconnected' : chain ? `Height: ${chain.height}` : 'Connecting...';

  const handleUnlock = async () => {
    if (!pw) return;
    setUnlockError('');
    const ok = await unlock(pw);
    if (ok) {
      setShowUnlock(false);
      setPw('');
    } else {
      setUnlockError('Wrong password');
    }
  };

  return (
    <>
      <div className="flex items-center justify-between px-6 py-4 border-b border-border bg-background">
        <h1 className="text-xl font-semibold tracking-tight">{title}</h1>
        <div className="flex items-center gap-3">
          {walletName && accounts.length > 0 && (
            <span className="text-sm font-mono font-medium text-primary">
              {trimAmount(balance.spendable)} KGX
            </span>
          )}
          {walletName && (
            <span className="text-sm text-muted-foreground">{walletName}</span>
          )}
          {walletName && !unlocked && (
            <Button variant="outline" size="sm" onClick={() => setShowUnlock(true)}>
              <Unlock className="h-3.5 w-3.5 mr-1.5" />
              Unlock
            </Button>
          )}
          {walletName && unlocked && (
            <Button variant="ghost" size="sm" onClick={lock}>
              <Lock className="h-3.5 w-3.5 mr-1.5" />
              Lock
            </Button>
          )}
          <span className="text-sm text-muted-foreground">{statusText}</span>
          <span className={cn('h-2.5 w-2.5 rounded-full', statusClass)} />
          <ThemeToggle />
        </div>
      </div>

      <Dialog open={showUnlock} onOpenChange={(open: boolean) => { if (!open) { setShowUnlock(false); setPw(''); setUnlockError(''); } }}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Unlock Wallet</DialogTitle>
            <DialogDescription>
              Enter password for <strong>{walletName}</strong>
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-2">
            <div className="space-y-2">
              <Label htmlFor="unlock-pw">Password</Label>
              <Input
                id="unlock-pw"
                type="password"
                value={pw}
                onChange={(e: React.ChangeEvent<HTMLInputElement>) => setPw(e.target.value)}
                onKeyDown={(e: React.KeyboardEvent<HTMLInputElement>) => e.key === 'Enter' && handleUnlock()}
                placeholder="Wallet password"
                autoFocus
              />
            </div>
            {unlockError && (
              <Alert variant="destructive">
                <AlertDescription>{unlockError}</AlertDescription>
              </Alert>
            )}
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => { setShowUnlock(false); setPw(''); setUnlockError(''); }}>
              Cancel
            </Button>
            <Button onClick={handleUnlock}>Unlock</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
