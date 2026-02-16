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
  const [rpcEndpoint, setRpcEndpoint] = useState('');
  const [dataDir, setDataDir] = useState('');
  const [network, setNetwork] = useState('mainnet');
  const [testResult, setTestResult] = useState<'success' | 'error' | null>(null);
  const [testing, setTesting] = useState(false);
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    (async () => {
      try {
        const mod = await import('../../../wailsjs/go/main/App');
        setRpcEndpoint(await mod.GetRPCEndpoint());
        setDataDir(await mod.GetDataDir());
        setNetwork(await mod.GetNetwork());
      } catch {
        // ignore
      }
    })();
  }, []);

  const handleSave = async () => {
    try {
      const mod = await import('../../../wailsjs/go/main/App');
      await mod.SetRPCEndpoint(rpcEndpoint);
      await mod.SetDataDir(dataDir);
      await mod.SetNetwork(network);
      // Reload wallet list with updated network/endpoint.
      lock();
      await refreshWallets();
      setSaved(true);
      setTimeout(() => setSaved(false), 2000);
    } catch {
      // ignore
    }
  };

  const handleTest = async () => {
    setTesting(true);
    setTestResult(null);
    try {
      const mod = await import('../../../wailsjs/go/main/App');
      await mod.SetRPCEndpoint(rpcEndpoint);
      const ok = await mod.TestConnection();
      setTestResult(ok ? 'success' : 'error');
    } catch {
      setTestResult('error');
    } finally {
      setTesting(false);
    }
  };

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader>
          <CardTitle>Node Connection</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="space-y-2">
            <Label>RPC Endpoint</Label>
            <Input
              type="text"
              value={rpcEndpoint}
              onChange={(e: React.ChangeEvent<HTMLInputElement>) => setRpcEndpoint(e.target.value)}
              placeholder="http://127.0.0.1:8545"
            />
          </div>
          <div className="space-y-2">
            <Label>Data Directory</Label>
            <Input
              type="text"
              value={dataDir}
              onChange={(e: React.ChangeEvent<HTMLInputElement>) => setDataDir(e.target.value)}
            />
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
          </div>
          <div className="flex gap-2">
            <Button onClick={handleSave}>
              {saved ? 'Saved!' : 'Save'}
            </Button>
            <Button variant="outline" onClick={handleTest} disabled={testing}>
              {testing ? 'Testing...' : 'Test Connection'}
            </Button>
          </div>
          {testResult === 'success' && (
            <Alert className="mt-2">
              <AlertDescription>Connected successfully!</AlertDescription>
            </Alert>
          )}
          {testResult === 'error' && (
            <Alert variant="destructive" className="mt-2">
              <AlertDescription>Connection failed. Check endpoint and ensure klingnetd is running.</AlertDescription>
            </Alert>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
