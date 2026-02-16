import { Card, CardContent } from '@/components/ui/card';
import { Wallet, Lock } from 'lucide-react';

interface StatusGuardProps {
  walletName: string | null;
  unlocked: boolean;
  children: React.ReactNode;
}

export function StatusGuard({ walletName, unlocked, children }: StatusGuardProps) {
  if (!walletName) {
    return (
      <Card>
        <CardContent className="flex flex-col items-center justify-center py-12 text-muted-foreground gap-3">
          <Wallet className="h-8 w-8" />
          <p>Select a wallet in Settings first.</p>
        </CardContent>
      </Card>
    );
  }

  if (!unlocked) {
    return (
      <Card>
        <CardContent className="flex flex-col items-center justify-center py-12 text-muted-foreground gap-3">
          <Lock className="h-8 w-8" />
          <p>Unlock your wallet using the button in the header.</p>
        </CardContent>
      </Card>
    );
  }

  return <>{children}</>;
}
