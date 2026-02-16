import { useEffect } from 'react';
import { BrowserRouter, Routes, Route, useLocation } from 'react-router-dom';
import { WalletProvider } from './context/WalletContext';
import Sidebar from './components/Layout/Sidebar';
import Header from './components/Layout/Header';
import Dashboard from './components/Dashboard/Dashboard';
import CreateWallet from './components/Wallet/CreateWallet';
import Wallets from './components/Wallet/Wallets';
import Send from './components/Wallet/Send';
import SendMany from './components/Wallet/SendMany';
import Receive from './components/Wallet/Receive';
import MintToken from './components/Wallet/MintToken';
import ExportKey from './components/Wallet/ExportKey';
import TransactionList from './components/Transactions/TransactionList';
import NetworkStatus from './components/Network/NetworkStatus';
import StakingView from './components/Staking/StakingView';
import StakeCreate from './components/Staking/StakeCreate';
import StakeUnstake from './components/Staking/StakeUnstake';
import Tokens from './components/Tokens/Tokens';
import SendToken from './components/Tokens/SendToken';
import SubChainList from './components/SubChains/SubChainList';
import SubChainSend from './components/SubChains/SubChainSend';
import SubChainStake from './components/SubChains/SubChainStake';
import CreateSubChain from './components/SubChains/CreateSubChain';
import Settings from './components/Settings/Settings';

const pageTitles: Record<string, string> = {
  '/': 'Dashboard',
  '/wallets': 'Wallets',
  '/wallet/create': 'Create Wallet',
  '/send': 'Send',
  '/send-many': 'Send Many',
  '/receive': 'Receive',
  '/mint-token': 'Mint Token',
  '/export-key': 'Export Key',
  '/transactions': 'Transactions',
  '/network': 'Network',
  '/staking': 'Staking',
  '/stake-create': 'Create Stake',
  '/stake-unstake': 'Unstake Validator',
  '/tokens': 'My Tokens',
  '/token-send': 'Send Token',
  '/subchains': 'Sub-Chains',
  '/subchain-send': 'Sub-Chain Send',
  '/subchain-stake': 'Sub-Chain Stake',
  '/subchain-create': 'Create Sub-Chain',
  '/settings': 'Settings',
};

function AppContent() {
  const location = useLocation();
  const title = pageTitles[location.pathname] || 'Klingnet Wallet';

  return (
    <div className="flex h-screen bg-background">
      <Sidebar />
      <div className="flex-1 flex flex-col overflow-hidden">
        <Header title={title} />
        <main className="flex-1 overflow-y-auto p-6">
          <div className="max-w-5xl mx-auto space-y-6">
            <Routes>
              <Route path="/" element={<Dashboard />} />
              <Route path="/wallets" element={<Wallets />} />
              <Route path="/wallet/create" element={<CreateWallet />} />
              <Route path="/send" element={<Send />} />
              <Route path="/send-many" element={<SendMany />} />
              <Route path="/receive" element={<Receive />} />
              <Route path="/mint-token" element={<MintToken />} />
              <Route path="/export-key" element={<ExportKey />} />
              <Route path="/transactions" element={<TransactionList />} />
              <Route path="/network" element={<NetworkStatus />} />
              <Route path="/staking" element={<StakingView />} />
              <Route path="/stake-create" element={<StakeCreate />} />
              <Route path="/stake-unstake" element={<StakeUnstake />} />
              <Route path="/tokens" element={<Tokens />} />
              <Route path="/token-send" element={<SendToken />} />
              <Route path="/subchains" element={<SubChainList />} />
              <Route path="/subchain-send" element={<SubChainSend />} />
              <Route path="/subchain-stake" element={<SubChainStake />} />
              <Route path="/subchain-create" element={<CreateSubChain />} />
              <Route path="/settings" element={<Settings />} />
            </Routes>
          </div>
        </main>
      </div>
    </div>
  );
}

export default function App() {
  useEffect(() => {
    const stored = localStorage.getItem('theme');
    if (stored === 'dark' || (!stored && window.matchMedia('(prefers-color-scheme: dark)').matches)) {
      document.documentElement.classList.add('dark');
    }
  }, []);

  return (
    <BrowserRouter>
      <WalletProvider>
        <AppContent />
      </WalletProvider>
    </BrowserRouter>
  );
}
