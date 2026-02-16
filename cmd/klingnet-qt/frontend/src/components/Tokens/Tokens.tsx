import { useWallet } from '../../context/WalletContext';
import { usePolling } from '../../hooks/usePolling';
import { truncateHash } from '../../utils/format';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { Alert, AlertDescription } from '@/components/ui/alert';
import CopyButton from '../ui/CopyButton';

interface TokenBalance {
  token_id: string;
  amount: number;
  name: string;
  symbol: string;
  decimals: number;
}

export default function Tokens() {
  const { walletName, accounts, unlocked } = useWallet();

  const addresses = accounts.map((a) => a.address);

  const { data: balances, error } = usePolling<TokenBalance[]>(async () => {
    if (addresses.length === 0) return [];
    const mod = await import('../../../wailsjs/go/main/WalletService');
    return mod.GetTokenBalances(addresses);
  }, 10000, unlocked && addresses.length > 0);

  if (!walletName) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>My Tokens</CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-muted-foreground">No wallet selected. Select a wallet in Settings.</p>
        </CardContent>
      </Card>
    );
  }

  if (!unlocked) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>My Tokens</CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-muted-foreground">Wallet is locked. Unlock in the header to view token balances.</p>
        </CardContent>
      </Card>
    );
  }

  if (error) {
    return (
      <Alert variant="destructive">
        <AlertDescription>Unable to load token balances: {error}</AlertDescription>
      </Alert>
    );
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>My Tokens</CardTitle>
      </CardHeader>
      <CardContent>
        {balances && balances.length > 0 ? (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Symbol</TableHead>
                <TableHead>Name</TableHead>
                <TableHead>Balance</TableHead>
                <TableHead>Token ID</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {balances.map((t) => (
                <TableRow key={t.token_id}>
                  <TableCell className="font-semibold">{t.symbol || '---'}</TableCell>
                  <TableCell>{t.name || '---'}</TableCell>
                  <TableCell>{t.amount}</TableCell>
                  <TableCell>
                    <div className="flex items-center font-mono">
                      <span>{truncateHash(t.token_id, 12)}</span>
                      <CopyButton text={t.token_id} />
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        ) : (
          <p className="text-muted-foreground">No tokens found. Mint or receive tokens to see them here.</p>
        )}
      </CardContent>
    </Card>
  );
}
