import { useState, useEffect } from 'react';
import { useWallet } from '../../context/WalletContext';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import type { NotificationSettings } from '../../utils/types';

export default function Settings() {
  const { refreshWallets, lock } = useWallet();
  const [dataDir, setDataDir] = useState('');
  const [network, setNetwork] = useState('mainnet');
  const [notifySettings, setNotifySettings] = useState<NotificationSettings>({
    mined: true, sent: true, received: true, token_sent: true, token_received: true,
  });
  const [confFilePath, setConfFilePath] = useState('');
  const [startupError, setStartupError] = useState('');
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    (async () => {
      try {
        const mod = await import('../../../wailsjs/go/main/App');
        setDataDir(await mod.GetDataDir());
        setNetwork(await mod.GetNetwork());
        setNotifySettings(await mod.GetNotificationSettings());
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
      await mod.SetNotificationSettings(notifySettings);
      // Reload wallet list with updated network.
      lock();
      await refreshWallets();
      setSaved(true);
      setTimeout(() => setSaved(false), 2000);
    } catch {
      // ignore
    }
  };

  const toggleNotify = (key: keyof NotificationSettings) => {
    setNotifySettings((prev) => ({ ...prev, [key]: !prev[key] }));
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
            <div className="space-y-1 pl-1">
              {([
                ['mined', 'Block rewards (mined)'],
                ['sent', 'KGX sent'],
                ['received', 'KGX received'],
                ['token_sent', 'Token sent'],
                ['token_received', 'Token received'],
              ] as [keyof NotificationSettings, string][]).map(([key, label]) => (
                <label key={key} className="flex items-center gap-2 text-sm cursor-pointer">
                  <input
                    type="checkbox"
                    checked={notifySettings[key]}
                    onChange={() => toggleNotify(key)}
                    className="rounded border-input"
                  />
                  {label}
                </label>
              ))}
            </div>
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
