import { useState } from 'react';
import { Link } from 'react-router-dom';
import {
  Box,
  Search,
  Filter,
  MoreHorizontal,
  Trash2,
  Terminal,
  ExternalLink,
  Plus,
  RefreshCw,
} from 'lucide-react';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Select } from '@/components/ui/Select';
import { StateBadge } from '@/components/ui/Badge';
import { LoadingState } from '@/components/ui/Spinner';
import { EmptyState } from '@/components/ui/EmptyState';
import { useSandboxes, useTerminateSandbox } from '@/hooks/useSandboxes';
import { formatRelativeTime, formatDate } from '@/lib/utils';
import type { SandboxState } from '@/types/sandbox';
import { CreateSandboxModal } from './CreateSandboxModal';

const stateOptions = [
  { value: '', label: 'All States' },
  { value: 'running', label: 'Running' },
  { value: 'pending', label: 'Pending' },
  { value: 'starting', label: 'Starting' },
  { value: 'stopped', label: 'Stopped' },
  { value: 'error', label: 'Error' },
];

function SandboxList() {
  const [stateFilter, setStateFilter] = useState<SandboxState | ''>('');
  const [searchQuery, setSearchQuery] = useState('');
  const [isCreateModalOpen, setIsCreateModalOpen] = useState(false);
  const [openMenuId, setOpenMenuId] = useState<string | null>(null);

  const { data, isLoading, error, refetch, isFetching } = useSandboxes({
    state: stateFilter || undefined,
    limit: 50,
  });

  const terminateMutation = useTerminateSandbox();

  const sandboxes = (data?.items || []).filter((s) =>
    s.name.toLowerCase().includes(searchQuery.toLowerCase())
  );

  const handleTerminate = async (id: string) => {
    if (confirm('Are you sure you want to terminate this sandbox?')) {
      await terminateMutation.mutateAsync({ id });
    }
    setOpenMenuId(null);
  };

  if (isLoading) {
    return <LoadingState message="Loading sandboxes..." />;
  }

  if (error) {
    return (
      <EmptyState
        title="Failed to load sandboxes"
        description={(error as Error).message}
        action={
          <Button onClick={() => refetch()}>
            <RefreshCw className="mr-2 h-4 w-4" />
            Retry
          </Button>
        }
      />
    );
  }

  return (
    <div className="space-y-6">
      {/* Page header */}
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Sandboxes</h1>
          <p className="text-muted-foreground mt-1">
            Manage your sandbox environments
          </p>
        </div>
        <Button onClick={() => setIsCreateModalOpen(true)}>
          <Plus className="mr-2 h-4 w-4" />
          New Sandbox
        </Button>
      </div>

      {/* Filters */}
      <Card className="p-4">
        <div className="flex flex-col gap-4 sm:flex-row sm:items-center">
          <div className="relative flex-1">
            <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              placeholder="Search sandboxes..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="pl-9"
            />
          </div>
          <div className="flex items-center gap-2">
            <Filter className="h-4 w-4 text-muted-foreground" />
            <Select
              options={stateOptions}
              value={stateFilter}
              onChange={(e) => setStateFilter(e.target.value as SandboxState | '')}
              className="w-36"
            />
          </div>
          <Button
            variant="outline"
            size="icon"
            onClick={() => refetch()}
            disabled={isFetching}
          >
            <RefreshCw className={`h-4 w-4 ${isFetching ? 'animate-spin' : ''}`} />
          </Button>
        </div>
      </Card>

      {/* Sandbox list */}
      {sandboxes.length === 0 ? (
        <EmptyState
          icon={<Box className="h-8 w-8 text-muted-foreground" />}
          title="No sandboxes found"
          description={
            searchQuery || stateFilter
              ? 'Try adjusting your filters'
              : 'Create your first sandbox to get started'
          }
          action={
            !searchQuery && !stateFilter ? (
              <Button onClick={() => setIsCreateModalOpen(true)}>
                <Plus className="mr-2 h-4 w-4" />
                Create Sandbox
              </Button>
            ) : undefined
          }
        />
      ) : (
        <div className="grid gap-4">
          {sandboxes.map((sandbox) => (
            <Card
              key={sandbox.id}
              className="p-4 hover:shadow-md transition-shadow"
            >
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-4">
                  <div className="flex h-12 w-12 items-center justify-center rounded-lg bg-muted">
                    <Box className="h-6 w-6" />
                  </div>
                  <div>
                    <div className="flex items-center gap-3">
                      <Link
                        to={`/sandboxes/${sandbox.id}`}
                        className="font-semibold hover:underline"
                      >
                        {sandbox.name}
                      </Link>
                      <StateBadge state={sandbox.state as SandboxState} />
                    </div>
                    <div className="flex items-center gap-4 mt-1 text-sm text-muted-foreground">
                      <span title={formatDate(sandbox.createdAt)}>
                        Created {formatRelativeTime(sandbox.createdAt)}
                      </span>
                      <span className="font-mono text-xs">
                        {sandbox.id.slice(0, 8)}...
                      </span>
                    </div>
                  </div>
                </div>

                <div className="flex items-center gap-2">
                  {sandbox.state === 'running' && (
                    <Link to={`/sandboxes/${sandbox.id}`}>
                      <Button variant="outline" size="sm">
                        <Terminal className="mr-2 h-4 w-4" />
                        Terminal
                      </Button>
                    </Link>
                  )}

                  <div className="relative">
                    <Button
                      variant="ghost"
                      size="icon"
                      onClick={() =>
                        setOpenMenuId(openMenuId === sandbox.id ? null : sandbox.id)
                      }
                    >
                      <MoreHorizontal className="h-4 w-4" />
                    </Button>

                    {openMenuId === sandbox.id && (
                      <>
                        <div
                          className="fixed inset-0 z-10"
                          onClick={() => setOpenMenuId(null)}
                        />
                        <div className="absolute right-0 z-20 mt-2 w-48 rounded-md border bg-background shadow-lg">
                          <div className="p-1">
                            <Link
                              to={`/sandboxes/${sandbox.id}`}
                              className="flex items-center gap-2 w-full px-3 py-2 text-sm rounded hover:bg-muted"
                              onClick={() => setOpenMenuId(null)}
                            >
                              <ExternalLink className="h-4 w-4" />
                              View Details
                            </Link>
                            {sandbox.state === 'running' && (
                              <Link
                                to={`/sandboxes/${sandbox.id}`}
                                className="flex items-center gap-2 w-full px-3 py-2 text-sm rounded hover:bg-muted"
                                onClick={() => setOpenMenuId(null)}
                              >
                                <Terminal className="h-4 w-4" />
                                Open Terminal
                              </Link>
                            )}
                            <button
                              className="flex items-center gap-2 w-full px-3 py-2 text-sm rounded hover:bg-muted text-destructive"
                              onClick={() => handleTerminate(sandbox.id)}
                              disabled={terminateMutation.isPending}
                            >
                              <Trash2 className="h-4 w-4" />
                              Terminate
                            </button>
                          </div>
                        </div>
                      </>
                    )}
                  </div>
                </div>
              </div>

              {/* Error message if any */}
              {sandbox.errorReason && (
                <div className="mt-3 p-3 rounded-lg bg-destructive/10 text-destructive text-sm">
                  {sandbox.errorReason}
                </div>
              )}

              {/* Environment variables preview */}
              {sandbox.env && Object.keys(sandbox.env).length > 0 && (
                <div className="mt-3 flex flex-wrap gap-2">
                  {Object.keys(sandbox.env)
                    .slice(0, 3)
                    .map((key) => (
                      <span
                        key={key}
                        className="inline-flex items-center rounded-full bg-muted px-2.5 py-0.5 text-xs font-mono"
                      >
                        {key}
                      </span>
                    ))}
                  {Object.keys(sandbox.env).length > 3 && (
                    <span className="text-xs text-muted-foreground">
                      +{Object.keys(sandbox.env).length - 3} more
                    </span>
                  )}
                </div>
              )}
            </Card>
          ))}
        </div>
      )}

      {/* Pagination info */}
      {data && data.total > 0 && (
        <div className="text-sm text-muted-foreground text-center">
          Showing {sandboxes.length} of {data.total} sandboxes
        </div>
      )}

      {/* Create Modal */}
      <CreateSandboxModal
        isOpen={isCreateModalOpen}
        onClose={() => setIsCreateModalOpen(false)}
      />
    </div>
  );
}

export { SandboxList };
