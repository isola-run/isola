import { Link } from 'react-router-dom'
import { Box, ArrowRight, Plus, Zap, Shield, Terminal } from 'lucide-react'
import { useSandboxes } from '@/hooks/use-sandboxes'
import { StatusBadge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { formatRelativeTime } from '@/lib/utils'
import type { SandboxStatus } from '@/api/types'

const statusOrder: SandboxStatus[] = ['running', 'creating', 'shuttingDown', 'failed', 'stopped']

function StatCard({ label, count, accent }: { label: string; count: number; accent: string }) {
  return (
    <div className="bg-bg-surface border border-border-subtle rounded-xl p-4 hover:border-border-default transition-colors">
      <div className="flex items-center justify-between mb-2">
        <span className="text-xs font-medium text-text-tertiary uppercase tracking-wider">{label}</span>
        <span className={`w-2 h-2 rounded-full ${accent}`} />
      </div>
      <div className="text-2xl font-semibold text-text-primary tracking-tight">{count}</div>
    </div>
  )
}

export function DashboardPage() {
  const { data: sandboxes, isLoading } = useSandboxes()

  const counts = {
    total: sandboxes?.length ?? 0,
    running: sandboxes?.filter((s) => s.status === 'running').length ?? 0,
    creating: sandboxes?.filter((s) => s.status === 'creating').length ?? 0,
    failed: sandboxes?.filter((s) => s.status === 'failed').length ?? 0,
    stopped: sandboxes?.filter((s) => s.status === 'stopped').length ?? 0,
  }

  const recentSandboxes = [...(sandboxes ?? [])]
    .sort((a, b) => new Date(b.creationTimestamp).getTime() - new Date(a.creationTimestamp).getTime())
    .slice(0, 5)

  return (
    <div className="max-w-5xl mx-auto px-6 py-8 animate-fade-in">
      {/* Hero */}
      <div className="mb-8">
        <h1 className="text-xl font-semibold text-text-primary mb-1">Dashboard</h1>
        <p className="text-sm text-text-secondary">Overview of your sandbox environment</p>
      </div>

      {/* Stat cards */}
      {isLoading ? (
        <div className="grid grid-cols-2 md:grid-cols-4 gap-4 mb-8">
          {[...Array(4)].map((_, i) => (
            <Skeleton key={i} className="h-24 rounded-xl" />
          ))}
        </div>
      ) : (
        <div className="grid grid-cols-2 md:grid-cols-4 gap-4 mb-8">
          <StatCard label="Total" count={counts.total} accent="bg-accent" />
          <StatCard label="Running" count={counts.running} accent="bg-status-running" />
          <StatCard label="Creating" count={counts.creating} accent="bg-status-creating" />
          <StatCard label="Failed" count={counts.failed} accent="bg-status-failed" />
        </div>
      )}

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        {/* Recent sandboxes */}
        <div className="lg:col-span-2 bg-bg-surface border border-border-subtle rounded-xl">
          <div className="flex items-center justify-between px-4 py-3 border-b border-border-subtle">
            <h2 className="text-sm font-semibold text-text-primary">Recent Sandboxes</h2>
            <Link to="/sandboxes" className="text-xs text-accent hover:text-accent-hover transition-colors flex items-center gap-1">
              View all <ArrowRight className="w-3 h-3" />
            </Link>
          </div>

          {isLoading ? (
            <div className="p-4 space-y-3">
              {[...Array(3)].map((_, i) => (
                <Skeleton key={i} className="h-12 rounded-lg" />
              ))}
            </div>
          ) : recentSandboxes.length === 0 ? (
            <div className="p-8 text-center">
              <Box className="w-8 h-8 text-text-tertiary mx-auto mb-2" />
              <p className="text-sm text-text-secondary mb-3">No sandboxes yet</p>
              <Link to="/sandboxes?create=true">
                <Button variant="primary" size="sm">
                  <Plus className="w-3.5 h-3.5" />
                  Create your first sandbox
                </Button>
              </Link>
            </div>
          ) : (
            <div>
              {recentSandboxes.map((sb) => (
                <Link
                  key={sb.id}
                  to={`/sandboxes/${sb.id}`}
                  className="flex items-center justify-between px-4 py-3 hover:bg-bg-hover/50 transition-colors border-b border-border-subtle/50 last:border-b-0"
                >
                  <div className="flex items-center gap-3 min-w-0">
                    <StatusBadge status={sb.status} size="sm" />
                    <span className="font-mono text-xs text-text-primary truncate">{sb.id}</span>
                  </div>
                  <div className="flex items-center gap-3 text-xs text-text-tertiary shrink-0">
                    <span>{formatRelativeTime(sb.creationTimestamp)}</span>
                  </div>
                </Link>
              ))}
            </div>
          )}
        </div>

        {/* Quick actions */}
        <div className="space-y-4">
          <div className="bg-bg-surface border border-border-subtle rounded-xl p-4">
            <h2 className="text-sm font-semibold text-text-primary mb-3">Quick Actions</h2>
            <div className="space-y-2">
              <Link to="/sandboxes?create=true">
                <button className="w-full flex items-center gap-3 px-3 py-2.5 rounded-lg text-left hover:bg-bg-hover transition-colors group">
                  <div className="w-8 h-8 rounded-lg bg-accent/10 flex items-center justify-center shrink-0 group-hover:bg-accent/20 transition-colors">
                    <Plus className="w-4 h-4 text-accent" />
                  </div>
                  <div>
                    <div className="text-sm font-medium text-text-primary">New Sandbox</div>
                    <div className="text-xs text-text-tertiary">Launch an environment</div>
                  </div>
                </button>
              </Link>
            </div>
          </div>

          {/* Status summary */}
          <div className="bg-bg-surface border border-border-subtle rounded-xl p-4">
            <h2 className="text-sm font-semibold text-text-primary mb-3">By Status</h2>
            <div className="space-y-2">
              {statusOrder.map((status) => {
                const count = sandboxes?.filter((s) => s.status === status).length ?? 0
                if (count === 0) return null
                return (
                  <div key={status} className="flex items-center justify-between text-sm">
                    <StatusBadge status={status} size="sm" />
                    <span className="font-mono text-xs text-text-secondary">{count}</span>
                  </div>
                )
              })}
              {counts.total === 0 && (
                <p className="text-xs text-text-tertiary">No sandboxes</p>
              )}
            </div>
          </div>

          {/* Features overview */}
          <div className="bg-bg-surface border border-border-subtle rounded-xl p-4">
            <h2 className="text-sm font-semibold text-text-primary mb-3">Features</h2>
            <div className="space-y-2.5">
              {[
                { icon: Terminal, label: 'Execute commands', desc: 'Real-time streaming output' },
                { icon: Shield, label: 'Network isolation', desc: 'Per-sandbox policies' },
                { icon: Zap, label: 'Fast provisioning', desc: 'Seconds to start' },
              ].map(({ icon: Icon, label, desc }) => (
                <div key={label} className="flex items-start gap-2.5">
                  <Icon className="w-3.5 h-3.5 text-text-tertiary mt-0.5 shrink-0" />
                  <div>
                    <div className="text-xs font-medium text-text-secondary">{label}</div>
                    <div className="text-[11px] text-text-tertiary">{desc}</div>
                  </div>
                </div>
              ))}
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}
