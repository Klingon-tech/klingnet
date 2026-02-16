import { useState, useEffect, useCallback } from 'react';
import type { AccountInfo } from '../utils/types';

export function useWalletList() {
  const [wallets, setWallets] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);

  const refresh = useCallback(async () => {
    try {
      const mod = await import('../../wailsjs/go/main/WalletService');
      const list = await mod.ListWallets();
      setWallets(list || []);
    } catch {
      setWallets([]);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { refresh(); }, [refresh]);

  return { wallets, loading, refresh };
}

export function useWalletAccounts(walletName: string | null, password: string | null) {
  const [accounts, setAccounts] = useState<AccountInfo[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    if (!walletName || !password) {
      setAccounts([]);
      return;
    }
    setLoading(true);
    setError(null);
    try {
      const mod = await import('../../wailsjs/go/main/WalletService');
      const accts = await mod.GetWalletAccounts(walletName, password);
      setAccounts(accts || []);
    } catch (err: unknown) {
      setAccounts([]);
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, [walletName, password]);

  useEffect(() => { refresh(); }, [refresh]);

  return { accounts, loading, error, refresh };
}

export function useBalance(address: string | null) {
  const [balance, setBalance] = useState<string>('0.00');
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!address) {
      setBalance('0.00');
      return;
    }
    const fetch = async () => {
      try {
        const mod = await import('../../wailsjs/go/main/WalletService');
        const bal = await mod.GetBalance(address);
        setBalance(bal.spendable ?? '0.000000000000');
        setError(null);
      } catch (err: unknown) {
        setError(err instanceof Error ? err.message : String(err));
      }
    };
    fetch();
    const id = setInterval(fetch, 3000);
    return () => clearInterval(id);
  }, [address]);

  return { balance, error };
}
