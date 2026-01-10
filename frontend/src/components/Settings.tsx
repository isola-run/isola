import { useState, useEffect } from 'react';
import { Key, Save, Check, AlertCircle, Moon, Sun, Monitor } from 'lucide-react';
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { api } from '@/services/api';
import { useTheme } from '@/hooks/useTheme';
import { cn } from '@/lib/utils';

function Settings() {
  const [apiKey, setApiKey] = useState('');
  const [isSaved, setIsSaved] = useState(false);
  const [error, setError] = useState('');
  const { theme, setTheme } = useTheme();

  useEffect(() => {
    const savedKey = api.getApiKey();
    if (savedKey) {
      // Mask the key for display
      setApiKey(savedKey.length > 8 ? savedKey.slice(0, 8) + '...' : savedKey);
    }
  }, []);

  const handleSaveApiKey = () => {
    if (!apiKey.trim()) {
      setError('API key is required');
      return;
    }

    // Only save if it's not the masked version
    if (!apiKey.endsWith('...')) {
      api.setApiKey(apiKey.trim());
    }

    setError('');
    setIsSaved(true);
    setTimeout(() => setIsSaved(false), 2000);
  };

  const handleClearApiKey = () => {
    api.setApiKey('');
    setApiKey('');
    setError('');
  };

  const themeOptions = [
    { value: 'light' as const, label: 'Light', icon: Sun },
    { value: 'dark' as const, label: 'Dark', icon: Moon },
    { value: 'system' as const, label: 'System', icon: Monitor },
  ];

  return (
    <div className="space-y-8 max-w-2xl">
      {/* Page header */}
      <div>
        <h1 className="text-3xl font-bold tracking-tight">Settings</h1>
        <p className="text-muted-foreground mt-1">
          Configure your Isola preferences
        </p>
      </div>

      {/* API Key Settings */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Key className="h-5 w-5" />
            API Key
          </CardTitle>
          <CardDescription>
            Your API key is used to authenticate requests to the Isola gateway
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <Input
            type="password"
            placeholder="iso_sk_..."
            value={apiKey}
            onChange={(e) => {
              setApiKey(e.target.value);
              setError('');
            }}
            error={error}
          />

          <div className="flex items-center gap-2">
            <Button onClick={handleSaveApiKey} disabled={isSaved}>
              {isSaved ? (
                <>
                  <Check className="mr-2 h-4 w-4" />
                  Saved
                </>
              ) : (
                <>
                  <Save className="mr-2 h-4 w-4" />
                  Save API Key
                </>
              )}
            </Button>
            <Button variant="outline" onClick={handleClearApiKey}>
              Clear
            </Button>
          </div>

          <div className="text-sm text-muted-foreground p-3 rounded-lg bg-muted">
            <p className="font-medium">Demo API Keys:</p>
            <ul className="mt-1 space-y-1 font-mono text-xs">
              <li>iso_sk_demo</li>
              <li>iso_sk_a1b2c3d4e5f67890a1b2c3d4e5f67890</li>
            </ul>
          </div>
        </CardContent>
      </Card>

      {/* Appearance Settings */}
      <Card>
        <CardHeader>
          <CardTitle>Appearance</CardTitle>
          <CardDescription>
            Customize how Isola looks on your device
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-3 gap-3">
            {themeOptions.map((option) => (
              <button
                key={option.value}
                onClick={() => setTheme(option.value)}
                className={cn(
                  'flex flex-col items-center gap-2 p-4 rounded-lg border-2 transition-colors',
                  theme === option.value
                    ? 'border-primary bg-primary/5'
                    : 'border-transparent bg-muted hover:border-muted-foreground/25'
                )}
              >
                <option.icon className="h-6 w-6" />
                <span className="text-sm font-medium">{option.label}</span>
              </button>
            ))}
          </div>
        </CardContent>
      </Card>

      {/* About Section */}
      <Card>
        <CardHeader>
          <CardTitle>About Isola</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="flex items-center justify-between">
            <span className="text-sm text-muted-foreground">Version</span>
            <span className="font-mono text-sm">0.1.0</span>
          </div>
          <div className="flex items-center justify-between">
            <span className="text-sm text-muted-foreground">Frontend</span>
            <span className="font-mono text-sm">React + TypeScript</span>
          </div>
          <div className="flex items-center justify-between">
            <span className="text-sm text-muted-foreground">API Version</span>
            <span className="font-mono text-sm">v1</span>
          </div>
        </CardContent>
      </Card>

      {/* Gateway Connection Status */}
      <Card>
        <CardHeader>
          <CardTitle>Gateway Connection</CardTitle>
          <CardDescription>
            Status of your connection to the Isola gateway
          </CardDescription>
        </CardHeader>
        <CardContent>
          <GatewayStatus />
        </CardContent>
      </Card>
    </div>
  );
}

function GatewayStatus() {
  const [status, setStatus] = useState<'checking' | 'connected' | 'error'>('checking');
  const [version, setVersion] = useState('');
  const [error, setError] = useState('');

  useEffect(() => {
    const checkStatus = async () => {
      try {
        const response = await fetch('/health');
        if (response.ok) {
          const data = await response.json();
          setStatus('connected');
          setVersion(data.version || 'unknown');
        } else {
          setStatus('error');
          setError(`HTTP ${response.status}`);
        }
      } catch (err) {
        setStatus('error');
        setError((err as Error).message);
      }
    };

    checkStatus();
    const interval = setInterval(checkStatus, 30000);
    return () => clearInterval(interval);
  }, []);

  return (
    <div className="flex items-center gap-3">
      <div
        className={cn(
          'flex h-10 w-10 items-center justify-center rounded-full',
          status === 'connected' && 'bg-emerald-100 dark:bg-emerald-900/30',
          status === 'error' && 'bg-destructive/20',
          status === 'checking' && 'bg-muted'
        )}
      >
        {status === 'connected' && (
          <Check className="h-5 w-5 text-emerald-600 dark:text-emerald-400" />
        )}
        {status === 'error' && (
          <AlertCircle className="h-5 w-5 text-destructive" />
        )}
        {status === 'checking' && (
          <div className="h-5 w-5 border-2 border-primary border-t-transparent rounded-full animate-spin" />
        )}
      </div>

      <div>
        <p className="font-medium">
          {status === 'connected' && 'Connected'}
          {status === 'error' && 'Connection Error'}
          {status === 'checking' && 'Checking...'}
        </p>
        <p className="text-sm text-muted-foreground">
          {status === 'connected' && `Gateway version ${version}`}
          {status === 'error' && error}
          {status === 'checking' && 'Connecting to gateway...'}
        </p>
      </div>
    </div>
  );
}

export { Settings };
