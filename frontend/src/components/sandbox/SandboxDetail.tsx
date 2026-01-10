import { useState } from 'react';
import { useParams, useNavigate, Link } from 'react-router-dom';
import {
  ArrowLeft,
  Box,
  Terminal,
  FolderOpen,
  Settings,
  Trash2,
  Copy,
  Check,
  Clock,
  AlertCircle,
  RefreshCw,
  Info,
} from 'lucide-react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { StateBadge } from '@/components/ui/Badge';
import { LoadingState } from '@/components/ui/Spinner';
import { useSandbox, useTerminateSandbox } from '@/hooks/useSandboxes';
import { formatDate, formatRelativeTime, cn } from '@/lib/utils';
import { SandboxTerminal } from './SandboxTerminal';
import { SandboxFiles } from './SandboxFiles';
import type { SandboxState } from '@/types/sandbox';

type Tab = 'terminal' | 'files' | 'settings';

function SandboxDetail() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [activeTab, setActiveTab] = useState<Tab>('terminal');
  const [copied, setCopied] = useState(false);

  const { data: sandbox, isLoading, error, refetch } = useSandbox(id!);
  const terminateMutation = useTerminateSandbox();

  const copyId = async () => {
    if (id) {
      await navigator.clipboard.writeText(id);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  };

  const handleTerminate = async () => {
    if (confirm('Are you sure you want to terminate this sandbox? This action cannot be undone.')) {
      await terminateMutation.mutateAsync({ id: id! });
      navigate('/sandboxes');
    }
  };

  if (isLoading) {
    return <LoadingState message="Loading sandbox..." />;
  }

  if (error || !sandbox) {
    return (
      <div className="flex flex-col items-center justify-center py-12">
        <AlertCircle className="h-12 w-12 text-destructive mb-4" />
        <p className="text-lg font-medium">Sandbox not found</p>
        <p className="text-sm text-muted-foreground mt-1">
          {(error as Error)?.message || 'The sandbox may have been deleted'}
        </p>
        <Link to="/sandboxes" className="mt-4">
          <Button>
            <ArrowLeft className="mr-2 h-4 w-4" />
            Back to Sandboxes
          </Button>
        </Link>
      </div>
    );
  }

  const tabs = [
    { id: 'terminal' as Tab, label: 'Terminal', icon: Terminal, disabled: sandbox.state !== 'running' },
    { id: 'files' as Tab, label: 'Files', icon: FolderOpen, disabled: sandbox.state !== 'running' },
    { id: 'settings' as Tab, label: 'Settings', icon: Settings, disabled: false },
  ];

  return (
    <div className="space-y-6">
      {/* Breadcrumb */}
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        <Link to="/sandboxes" className="hover:text-foreground transition-colors">
          Sandboxes
        </Link>
        <span>/</span>
        <span className="text-foreground">{sandbox.name}</span>
      </div>

      {/* Header */}
      <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div className="flex items-start gap-4">
          <div className="flex h-14 w-14 items-center justify-center rounded-xl bg-gradient-to-br from-indigo-500 to-purple-600 shadow-lg shadow-indigo-500/25">
            <Box className="h-7 w-7 text-white" />
          </div>
          <div>
            <div className="flex items-center gap-3">
              <h1 className="text-2xl font-bold">{sandbox.name}</h1>
              <StateBadge state={sandbox.state as SandboxState} />
            </div>
            <div className="flex items-center gap-4 mt-1 text-sm text-muted-foreground">
              <button
                onClick={copyId}
                className="flex items-center gap-1 hover:text-foreground transition-colors font-mono"
              >
                {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
                {id?.slice(0, 8)}...
              </button>
              <span className="flex items-center gap-1">
                <Clock className="h-3 w-3" />
                Created {formatRelativeTime(sandbox.createdAt)}
              </span>
            </div>
          </div>
        </div>

        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="icon"
            onClick={() => refetch()}
            title="Refresh"
          >
            <RefreshCw className="h-4 w-4" />
          </Button>
          <Button
            variant="destructive"
            onClick={handleTerminate}
            isLoading={terminateMutation.isPending}
          >
            <Trash2 className="mr-2 h-4 w-4" />
            Terminate
          </Button>
        </div>
      </div>

      {/* Error message */}
      {sandbox.errorReason && (
        <div className="p-4 rounded-lg bg-destructive/10 border border-destructive/20 flex items-start gap-3">
          <AlertCircle className="h-5 w-5 text-destructive flex-shrink-0 mt-0.5" />
          <div>
            <p className="font-medium text-destructive">Sandbox Error</p>
            <p className="text-sm text-destructive/80 mt-1">{sandbox.errorReason}</p>
          </div>
        </div>
      )}

      {/* Tabs */}
      <div className="border-b">
        <nav className="flex gap-4" aria-label="Tabs">
          {tabs.map((tab) => (
            <button
              key={tab.id}
              onClick={() => !tab.disabled && setActiveTab(tab.id)}
              disabled={tab.disabled}
              className={cn(
                'flex items-center gap-2 px-1 py-3 text-sm font-medium border-b-2 transition-colors',
                activeTab === tab.id
                  ? 'border-primary text-primary'
                  : 'border-transparent text-muted-foreground hover:text-foreground hover:border-muted-foreground',
                tab.disabled && 'opacity-50 cursor-not-allowed'
              )}
            >
              <tab.icon className="h-4 w-4" />
              {tab.label}
            </button>
          ))}
        </nav>
      </div>

      {/* Tab content */}
      <div className="min-h-[500px]">
        {activeTab === 'terminal' && (
          sandbox.state === 'running' ? (
            <SandboxTerminal sandboxId={id!} />
          ) : (
            <div className="flex flex-col items-center justify-center py-12">
              <Terminal className="h-12 w-12 text-muted-foreground mb-4" />
              <p className="text-lg font-medium">Terminal Unavailable</p>
              <p className="text-sm text-muted-foreground mt-1">
                The sandbox must be running to access the terminal
              </p>
            </div>
          )
        )}

        {activeTab === 'files' && (
          sandbox.state === 'running' ? (
            <SandboxFiles sandboxId={id!} />
          ) : (
            <div className="flex flex-col items-center justify-center py-12">
              <FolderOpen className="h-12 w-12 text-muted-foreground mb-4" />
              <p className="text-lg font-medium">File Operations Unavailable</p>
              <p className="text-sm text-muted-foreground mt-1">
                The sandbox must be running to manage files
              </p>
            </div>
          )
        )}

        {activeTab === 'settings' && (
          <div className="space-y-6">
            {/* Sandbox Info */}
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  <Info className="h-5 w-5" />
                  Sandbox Information
                </CardTitle>
              </CardHeader>
              <CardContent>
                <dl className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                  <div>
                    <dt className="text-sm font-medium text-muted-foreground">ID</dt>
                    <dd className="mt-1 font-mono text-sm">{sandbox.id}</dd>
                  </div>
                  <div>
                    <dt className="text-sm font-medium text-muted-foreground">Name</dt>
                    <dd className="mt-1 text-sm">{sandbox.name}</dd>
                  </div>
                  <div>
                    <dt className="text-sm font-medium text-muted-foreground">State</dt>
                    <dd className="mt-1">
                      <StateBadge state={sandbox.state as SandboxState} />
                    </dd>
                  </div>
                  <div>
                    <dt className="text-sm font-medium text-muted-foreground">Desired State</dt>
                    <dd className="mt-1">
                      <StateBadge state={sandbox.desiredState as SandboxState} />
                    </dd>
                  </div>
                  <div>
                    <dt className="text-sm font-medium text-muted-foreground">Created At</dt>
                    <dd className="mt-1 text-sm">{formatDate(sandbox.createdAt)}</dd>
                  </div>
                </dl>
              </CardContent>
            </Card>

            {/* Environment Variables */}
            <Card>
              <CardHeader>
                <CardTitle>Environment Variables</CardTitle>
              </CardHeader>
              <CardContent>
                {sandbox.env && Object.keys(sandbox.env).length > 0 ? (
                  <div className="space-y-2">
                    {Object.entries(sandbox.env).map(([key, value]) => (
                      <div
                        key={key}
                        className="flex items-center gap-2 p-2 rounded-lg bg-muted font-mono text-sm"
                      >
                        <span className="font-semibold">{key}</span>
                        <span className="text-muted-foreground">=</span>
                        <span className="truncate">{value}</span>
                      </div>
                    ))}
                  </div>
                ) : (
                  <p className="text-sm text-muted-foreground">
                    No environment variables configured
                  </p>
                )}
              </CardContent>
            </Card>

            {/* Labels */}
            <Card>
              <CardHeader>
                <CardTitle>Labels</CardTitle>
              </CardHeader>
              <CardContent>
                {sandbox.labels && Object.keys(sandbox.labels).length > 0 ? (
                  <div className="flex flex-wrap gap-2">
                    {Object.entries(sandbox.labels).map(([key, value]) => (
                      <span
                        key={key}
                        className="inline-flex items-center rounded-full bg-muted px-3 py-1 text-xs font-mono"
                      >
                        {key}: {value}
                      </span>
                    ))}
                  </div>
                ) : (
                  <p className="text-sm text-muted-foreground">No labels configured</p>
                )}
              </CardContent>
            </Card>

            {/* Danger Zone */}
            <Card className="border-destructive/50">
              <CardHeader>
                <CardTitle className="text-destructive">Danger Zone</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="flex items-center justify-between">
                  <div>
                    <p className="font-medium">Terminate Sandbox</p>
                    <p className="text-sm text-muted-foreground">
                      Permanently delete this sandbox and all its data
                    </p>
                  </div>
                  <Button
                    variant="destructive"
                    onClick={handleTerminate}
                    isLoading={terminateMutation.isPending}
                  >
                    <Trash2 className="mr-2 h-4 w-4" />
                    Terminate
                  </Button>
                </div>
              </CardContent>
            </Card>
          </div>
        )}
      </div>
    </div>
  );
}

export { SandboxDetail };
