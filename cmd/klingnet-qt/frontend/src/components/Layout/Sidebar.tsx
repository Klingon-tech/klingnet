import { NavLink } from 'react-router-dom';
import { ScrollArea } from '@/components/ui/scroll-area';
import {
  LayoutDashboard,
  Wallet,
  Send,
  SendHorizontal,
  Download,
  ArrowLeftRight,
  Coins,
  KeyRound,
  Layers,
  SendToBack,
  Globe,
  Shield,
  PlusCircle,
  MinusCircle,
  Link,
  ChevronsLeftRightEllipsis,
  Landmark,
  Pickaxe,
  Settings,
} from 'lucide-react';
import { cn } from '@/lib/utils';

interface NavItemProps {
  to: string;
  icon: React.ReactNode;
  label: string;
}

function NavItem({ to, icon, label }: NavItemProps) {
  return (
    <NavLink
      to={to}
      className={({ isActive }) =>
        cn(
          'flex items-center gap-3 px-3 py-2 mx-2 rounded-lg text-sm transition-colors',
          isActive
            ? 'bg-primary text-primary-foreground font-medium'
            : 'text-sidebar-foreground/70 hover:bg-sidebar-accent hover:text-sidebar-foreground'
        )
      }
    >
      {icon}
      <span>{label}</span>
    </NavLink>
  );
}

function SectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <div className="px-5 pt-5 pb-1 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
      {children}
    </div>
  );
}

export default function Sidebar() {
  return (
    <div className="w-60 shrink-0 border-r border-sidebar-border bg-sidebar flex flex-col h-full overflow-hidden">
      <div className="flex items-center gap-2.5 px-5 py-5 border-b border-sidebar-border">
        <img src="/logo.png" alt="Klingnet" className="h-7 w-7" />
        <span className="font-bold text-lg tracking-tight">Klingnet</span>
      </div>

      <ScrollArea className="flex-1 min-h-0">
        <nav className="py-2">
          <SectionLabel>Overview</SectionLabel>
          <NavItem to="/" icon={<LayoutDashboard className="h-4 w-4" />} label="Dashboard" />

          <SectionLabel>Wallet</SectionLabel>
          <NavItem to="/wallets" icon={<Wallet className="h-4 w-4" />} label="Wallets" />
          <NavItem to="/send" icon={<Send className="h-4 w-4" />} label="Send" />
          <NavItem to="/send-many" icon={<SendHorizontal className="h-4 w-4" />} label="Send Many" />
          <NavItem to="/receive" icon={<Download className="h-4 w-4" />} label="Receive" />
          <NavItem to="/transactions" icon={<ArrowLeftRight className="h-4 w-4" />} label="Transactions" />
          <NavItem to="/export-key" icon={<KeyRound className="h-4 w-4" />} label="Export Key" />

          <SectionLabel>Tokens</SectionLabel>
          <NavItem to="/tokens" icon={<Layers className="h-4 w-4" />} label="My Tokens" />
          <NavItem to="/mint-token" icon={<Coins className="h-4 w-4" />} label="Mint Token" />
          <NavItem to="/token-send" icon={<SendToBack className="h-4 w-4" />} label="Send Token" />

          <SectionLabel>Blockchain</SectionLabel>
          <NavItem to="/network" icon={<Globe className="h-4 w-4" />} label="Network" />

          <SectionLabel>Staking</SectionLabel>
          <NavItem to="/staking" icon={<Shield className="h-4 w-4" />} label="Validators" />
          <NavItem to="/stake-create" icon={<PlusCircle className="h-4 w-4" />} label="Create Stake" />
          <NavItem to="/stake-unstake" icon={<MinusCircle className="h-4 w-4" />} label="Unstake" />

          <SectionLabel>Advanced</SectionLabel>
          <NavItem to="/subchains" icon={<Link className="h-4 w-4" />} label="Sub-Chains" />
          <NavItem to="/subchain-send" icon={<ChevronsLeftRightEllipsis className="h-4 w-4" />} label="Sub-Chain Send" />
          <NavItem to="/subchain-stake" icon={<Landmark className="h-4 w-4" />} label="Sub-Chain Stake" />
          <NavItem to="/subchain-create" icon={<Pickaxe className="h-4 w-4" />} label="Create Sub-Chain" />
          <NavItem to="/settings" icon={<Settings className="h-4 w-4" />} label="Settings" />
        </nav>
      </ScrollArea>
    </div>
  );
}
