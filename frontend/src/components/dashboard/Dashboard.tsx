import { Link } from 'react-router-dom';
import {
  Box,
  Activity,
  Clock,
  AlertCircle,
  ArrowUpRight,
  Cpu,
  MemoryStick,
  TrendingUp,
} from 'lucide-react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { StateBadge } from '@/components/ui/Badge';
import { LoadingState } from '@/components/ui/Spinner';
import { useSandboxes } from '@/hooks/useSandboxes';
import { formatRelativeTime } from '@/lib/utils';
import type { SandboxState } from '@/types/sandbox';

function Dashboard() {
  const { data, isLoading, error } = useSandboxes({ limit: 100 });

  if (isLoading) {
    return <LoadingState message="Loading dashboard..." />;
  }

  if (error) {
    return (
      <div className="flex flex-col items-center justify-center py-12">
        <AlertCircle className="h-12 w-12 text-destructive mb-4" />
        <p className="text-lg font-medium">Failed to load dashboard</p>
        <p className="text-sm text-muted-foreground mt-1">{(error as Error).message}</p>
      </div>
    );
  }

  const sandboxes = data?.items || [];
  const stats = {
    total: sandboxes.length,
    running: sandboxes.filter((s) => s.state === 'running').length,
    pending: sandboxes.filter((s) => s.state === 'pending' || s.state === 'starting').length,
    stopped: sandboxes.filter((s) => s.state === 'stopped').length,
    error: sandboxes.filter((s) => s.state === 'error').length,
  };

  const recentSandboxes = [...sandboxes]
    .sort((a, b) => new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime())
    .slice(0, 5);

  return (
    <div className="space-y-8">
      {/* Page header */}
      <div>
        <h1 className="text-3xl font-bold tracking-tight">Dashboard</h1>
        <p className="text-muted-foreground mt-1">
          Overview of your sandbox infrastructure
        </p>
      </div>

      {/* Stats grid */}
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Total Sandboxes</CardTitle>
            <Box className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{stats.total}</div>
            <p className="text-xs text-muted-foreground mt-1">
              Across all states
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Running</CardTitle>
            <Activity className="h-4 w-4 text-emerald-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-emerald-600 dark:text-emerald-400">
              {stats.running}
            </div>
            <p className="text-xs text-muted-foreground mt-1">
              Active sandboxes
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Pending</CardTitle>
            <Clock className="h-4 w-4 text-amber-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-amber-600 dark:text-amber-400">
              {stats.pending}
            </div>
            <p className="text-xs text-muted-foreground mt-1">
              Starting or initializing
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Errors</CardTitle>
            <AlertCircle className="h-4 w-4 text-red-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-red-600 dark:text-red-400">
              {stats.error}
            </div>
            <p className="text-xs text-muted-foreground mt-1">
              Require attention
            </p>
          </CardContent>
        </Card>
      </div>

      {/* Quick actions & Recent activity */}
      <div className="grid gap-6 lg:grid-cols-2">
        {/* Quick actions */}
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <TrendingUp className="h-5 w-5" />
              Quick Actions
            </CardTitle>
          </CardHeader>
          <CardContent className="grid gap-3">
            <Link to="/sandboxes">
              <Button variant="outline" className="w-full justify-between">
                <span className="flex items-center gap-2">
                  <Box className="h-4 w-4" />
                  View All Sandboxes
                </span>
                <ArrowUpRight className="h-4 w-4" />
              </Button>
            </Link>
            <Link to="/templates">
              <Button variant="outline" className="w-full justify-between">
                <span className="flex items-center gap-2">
                  <Cpu className="h-4 w-4" />
                  Manage Templates
                </span>
                <ArrowUpRight className="h-4 w-4" />
              </Button>
            </Link>
            <Link to="/settings">
              <Button variant="outline" className="w-full justify-between">
                <span className="flex items-center gap-2">
                  <MemoryStick className="h-4 w-4" />
                  Configure Settings
                </span>
                <ArrowUpRight className="h-4 w-4" />
              </Button>
            </Link>
          </CardContent>
        </Card>

        {/* Recent sandboxes */}
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center justify-between">
              <span className="flex items-center gap-2">
                <Clock className="h-5 w-5" />
                Recent Sandboxes
              </span>
              <Link to="/sandboxes">
                <Button variant="ghost" size="sm">
                  View all
                  <ArrowUpRight className="ml-1 h-3 w-3" />
                </Button>
              </Link>
            </CardTitle>
          </CardHeader>
          <CardContent>
            {recentSandboxes.length === 0 ? (
              <p className="text-sm text-muted-foreground py-4 text-center">
                No sandboxes yet. Create one to get started!
              </p>
            ) : (
              <div className="space-y-3">
                {recentSandboxes.map((sandbox) => (
                  <Link
                    key={sandbox.id}
                    to={`/sandboxes/${sandbox.id}`}
                    className="flex items-center justify-between p-3 rounded-lg border hover:bg-muted/50 transition-colors"
                  >
                    <div className="flex items-center gap-3">
                      <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-muted">
                        <Box className="h-4 w-4" />
                      </div>
                      <div>
                        <p className="font-medium text-sm">{sandbox.name}</p>
                        <p className="text-xs text-muted-foreground">
                          {formatRelativeTime(sandbox.createdAt)}
                        </p>
                      </div>
                    </div>
                    <StateBadge state={sandbox.state as SandboxState} />
                  </Link>
                ))}
              </div>
            )}
          </CardContent>
        </Card>
      </div>

      {/* State distribution */}
      <Card>
        <CardHeader>
          <CardTitle>Sandbox Distribution</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex items-center gap-4">
            <div className="flex-1 h-4 bg-muted rounded-full overflow-hidden flex">
              {stats.running > 0 && (
                <div
                  className="h-full bg-emerald-500"
                  style={{ width: `${(stats.running / Math.max(stats.total, 1)) * 100}%` }}
                  title={`Running: ${stats.running}`}
                />
              )}
              {stats.pending > 0 && (
                <div
                  className="h-full bg-amber-500"
                  style={{ width: `${(stats.pending / Math.max(stats.total, 1)) * 100}%` }}
                  title={`Pending: ${stats.pending}`}
                />
              )}
              {stats.stopped > 0 && (
                <div
                  className="h-full bg-gray-400"
                  style={{ width: `${(stats.stopped / Math.max(stats.total, 1)) * 100}%` }}
                  title={`Stopped: ${stats.stopped}`}
                />
              )}
              {stats.error > 0 && (
                <div
                  className="h-full bg-red-500"
                  style={{ width: `${(stats.error / Math.max(stats.total, 1)) * 100}%` }}
                  title={`Error: ${stats.error}`}
                />
              )}
            </div>
          </div>
          <div className="flex items-center justify-center gap-6 mt-4 text-sm">
            <div className="flex items-center gap-2">
              <span className="h-3 w-3 rounded-full bg-emerald-500" />
              <span className="text-muted-foreground">Running ({stats.running})</span>
            </div>
            <div className="flex items-center gap-2">
              <span className="h-3 w-3 rounded-full bg-amber-500" />
              <span className="text-muted-foreground">Pending ({stats.pending})</span>
            </div>
            <div className="flex items-center gap-2">
              <span className="h-3 w-3 rounded-full bg-gray-400" />
              <span className="text-muted-foreground">Stopped ({stats.stopped})</span>
            </div>
            <div className="flex items-center gap-2">
              <span className="h-3 w-3 rounded-full bg-red-500" />
              <span className="text-muted-foreground">Error ({stats.error})</span>
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}

export { Dashboard };
