import { useState, useEffect } from 'react';
import { useWallet } from '../../context/WalletContext';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';

export default function Settings() {
  const { refreshWallets, lock } = useWallet();
  const [dataDir, setDataDir] = useState('');
  const [network, setNetwork] = useState('mainnet');
  const [notifications, setNotifications] = useState(true);
  const [confFilePath, setConfFilePath] = useState('');
  const [startupError, setStartupError] = useState('');
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    (async () => {
      try {
        const mod = await import('../../../wailsjs/go/main/App');
        setDataDir(await mod.GetDataDir());
        setNetwork(await mod.GetNetwork());
        setNotifications(await mod.GetNotificationsEnabled());
        setConfFilePath(await mod.GetConfFilePath());
        const err = await mod.GetStartupError();
        if (err) setStartupError(err);
      } catch {
        // ignore
      }
    })();
  }, []);

  const handleSave = async () => {
    try {
      const mod = await import('../../../wailsjs/go/main/App');
      await mod.SetDataDir(dataDir);
      await mod.SetNetwork(network);
      await mod.SetNotificationsEnabled(notifications);
      // Reload wallet list with updated network.
      lock();
      await refreshWallets();
      setSaved(true);
      setTimeout(() => setSaved(false), 2000);
    } catch {
      // ignore
    }
  };

  return (
    <div className="space-y-6">
      {startupError && (
        <Alert variant="destructive">
          <AlertDescription>
            Node failed to start: {startupError}
          </AlertDescription>
        </Alert>
      )}
      <Card>
        <CardHeader>
          <CardTitle>Node Settings</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="space-y-2">
            <Label>Data Directory</Label>
            <Input
              type="text"
              value={dataDir}
              onChange={(e: React.ChangeEvent<HTMLInputElement>) => setDataDir(e.target.value)}
            />
            <p className="text-xs text-muted-foreground">Restart required after changing.</p>
          </div>
          <div className="space-y-2">
            <Label>Network</Label>
            <Select value={network} onValueChange={setNetwork}>
              <SelectTrigger>
                <SelectValue placeholder="Select network" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="mainnet">Mainnet</SelectItem>
                <SelectItem value="testnet">Testnet</SelectItem>
              </SelectContent>
            </Select>
            <p className="text-xs text-muted-foreground">Restart required after changing.</p>
          </div>
          <div className="space-y-2">
            <Label>Desktop Notifications</Label>
            <Select value={notifications ? 'enabled' : 'disabled'} onValueChange={(v) => setNotifications(v === 'enabled')}>
              <SelectTrigger>
                <SelectValue placeholder="Select notification mode" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="enabled">Enabled</SelectItem>
                <SelectItem value="disabled">Disabled</SelectItem>
              </SelectContent>
            </Select>
            <p className="text-xs text-muted-foreground">Notifies on sent, received, and mined block rewards.</p>
          </div>
          <div className="space-y-2">
            <Label>Config File</Label>
            <Input type="text" value={confFilePath} readOnly className="bg-muted" />
            <p className="text-xs text-muted-foreground">
              Advanced settings (mining, validator key, sub-chain sync) can be configured by editing klingnet.conf. Restart to apply.
            </p>
          </div>
          <div className="flex gap-2">
            <Button onClick={handleSave}>
              {saved ? 'Saved!' : 'Save'}
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
