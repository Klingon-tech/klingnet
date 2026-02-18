import { createContext, useContext, useState, useEffect, useCallback, useRef, type ReactNode } from 'react';
import type { AccountInfo, BalanceInfo, TxHistoryEntry } from '../utils/types';
import { entryNotificationKey, notifyEntry, shouldNotifyEntry } from '../lib/notifications';

const emptyBalance: BalanceInfo = {
  total: '0.000000000000',
  spendable: '0.000000000000',
  immature: '0.000000000000',
  staked: '0.000000000000',
  locked: '0.000000000000',
};

interface WalletState {
  walletName: string;       // active wallet (from settings)
  wallets: string[];        // all wallet names
  unlocked: boolean;
  password: string;         // in-memory only, never persisted
  accounts: AccountInfo[];  // populated after unlock (or from cache)
  balance: BalanceInfo;     // balance breakdown across all accounts
  unlock: (password: string) => Promise<boolean>;
  lock: () => void;
  setActiveWallet: (name: string) => Promise<void>;
  refreshWallets: () => Promise<void>;
  refreshAccounts: () => Promise<void>;
}

const WalletContext = createContext<WalletState>({
  walletName: '',
  wallets: [],
  unlocked: false,
  password: '',
  accounts: [],
  balance: emptyBalance,
  unlock: async () => false,
  lock: () => {},
  setActiveWallet: async () => {},
  refreshWallets: async () => {},
  refreshAccounts: async () => {},
});

export function useWallet() {
  return useContext(WalletContext);
}

export function WalletProvider({ children }: { children: ReactNode }) {
  const [walletName, setWalletName] = useState('');
  const [wallets, setWallets] = useState<string[]>([]);
  const [unlocked, setUnlocked] = useState(false);
  const [password, setPassword] = useState('');
  const [accounts, setAccounts] = useState<AccountInfo[]>([]);
  const [balance, setBalance] = useState<BalanceInfo>(emptyBalance);
  const [notificationsEnabled, setNotificationsEnabled] = useState(true);

  // Keep refs for values used in intervals to avoid stale closures.
  const passwordRef = useRef(password);
  const walletNameRef = useRef(walletName);
  const notificationsEnabledRef = useRef(notificationsEnabled);
  const notifiedEntriesRef = useRef<Set<string>>(new Set());
  const historyBootstrappedRef = useRef(false);
  passwordRef.current = password;
  walletNameRef.current = walletName;
  notificationsEnabledRef.current = notificationsEnabled;

  // Load wallet list + active wallet + cached accounts on mount.
  const refreshWallets = useCallback(async () => {
    try {
      const walletMod = await import('../../wailsjs/go/main/WalletService');
      const list = await walletMod.ListWallets();
      setWallets(list || []);
    } catch {
      setWallets([]);
    }
  }, []);

  useEffect(() => {
    (async () => {
      await refreshWallets();
      try {
        const appMod = await import('../../wailsjs/go/main/App');
        const active = await appMod.GetActiveWallet();
        setNotificationsEnabled(await appMod.GetNotificationsEnabled());
        if (active) {
          setWalletName(active);
          // Load cached accounts (addresses only, no keys) for balance display.
          const cached = await appMod.GetKnownAccounts(active);
          if (cached && cached.length > 0) {
            setAccounts(cached);
          }
        }
      } catch {
        // ignore
      }
    })();
  }, [refreshWallets]);

  useEffect(() => {
    historyBootstrappedRef.current = false;
    notifiedEntriesRef.current.clear();
  }, [walletName]);

  // Refresh accounts from RPC (picks up new change addresses).
  const refreshAccounts = useCallback(async () => {
    const name = walletNameRef.current;
    const pw = passwordRef.current;
    if (!name || !pw) return;
    try {
      const mod = await import('../../wailsjs/go/main/WalletService');
      const accts = await mod.GetWalletAccounts(name, pw);
      setAccounts(accts || []);
    } catch {
      // ignore — wallet may be locked or unavailable
    }
  }, []);

  // Poll balance (sum all accounts).
  // Also refresh accounts every ~30s when unlocked to pick up change addresses.
  useEffect(() => {
    if (accounts.length === 0) {
      setBalance(emptyBalance);
      return;
    }
    let tick = 0;
    const fetchBal = async () => {
      try {
        const mod = await import('../../wailsjs/go/main/WalletService');
        const addrs = accounts.map((a) => a.address);
        const breakdown = await mod.GetTotalBalance(addrs);
        setBalance({
          total: breakdown.total ?? '0.000000000000',
          spendable: breakdown.spendable ?? '0.000000000000',
          immature: breakdown.immature ?? '0.000000000000',
          staked: breakdown.staked ?? '0.000000000000',
          locked: breakdown.locked ?? '0.000000000000',
        });
      } catch {
        // ignore
      }
      // Refresh accounts every ~30s when unlocked (Argon2 is expensive).
      tick++;
      if (unlocked && tick % 10 === 0) {
        await refreshAccounts();
      }
    };
    fetchBal();
    const id = setInterval(fetchBal, 3000);
    return () => clearInterval(id);
  }, [unlocked, accounts, refreshAccounts]);

  const setActiveWallet = useCallback(async (name: string) => {
    setWalletName(name);
    setUnlocked(false);
    setPassword('');
    setBalance(emptyBalance);
    try {
      const appMod = await import('../../wailsjs/go/main/App');
      await appMod.SetActiveWallet(name);
      // Load cached accounts for the new wallet.
      const cached = await appMod.GetKnownAccounts(name);
      setAccounts(cached && cached.length > 0 ? cached : []);
    } catch {
      setAccounts([]);
    }
  }, []);

  const unlock = useCallback(async (pw: string): Promise<boolean> => {
    if (!walletName) return false;
    try {
      const mod = await import('../../wailsjs/go/main/WalletService');
      const accts = await mod.GetWalletAccounts(walletName, pw);
      setAccounts(accts || []);
      setPassword(pw);
      setUnlocked(true);
      return true;
    } catch {
      return false;
    }
  }, [walletName]);

  const lock = useCallback(() => {
    setUnlocked(false);
    setPassword('');
    // Keep accounts (from cache) so balance keeps showing.
  }, []);

  // Poll recent history and notify only for new sent/received entries.
  useEffect(() => {
    if (!walletName || !unlocked || !password) return;

    const changeAddrs = new Set(accounts.filter((a) => a.change === 1).map((a) => a.address));
    const scanAndNotify = async () => {
      if (!notificationsEnabledRef.current) return;
      try {
        const mod = await import('../../wailsjs/go/main/WalletService');
        const result = await mod.GetTransactionHistory(walletNameRef.current, passwordRef.current, 40, 0);
        const entries = (result?.entries || []) as TxHistoryEntry[];
        if (!historyBootstrappedRef.current) {
          for (const e of entries) {
            notifiedEntriesRef.current.add(entryNotificationKey(e));
          }
          historyBootstrappedRef.current = true;
          return;
        }
        for (const e of entries) {
          const key = entryNotificationKey(e);
          if (notifiedEntriesRef.current.has(key)) continue;
          notifiedEntriesRef.current.add(key);
          if (shouldNotifyEntry(e, changeAddrs)) {
            void notifyEntry(e);
          }
        }
      } catch {
        // ignore
      }
    };

    void scanAndNotify();
    const id = setInterval(scanAndNotify, 10000);
    return () => clearInterval(id);
  }, [walletName, unlocked, password, accounts]);

  return (
    <WalletContext.Provider value={{
      walletName, wallets, unlocked, password, accounts, balance,
      unlock, lock, setActiveWallet, refreshWallets, refreshAccounts,
    }}>
      {children}
    </WalletContext.Provider>
  );
}
