import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import {
  Box,
  Play,
  Square,
  AlertCircle,
  ArrowRight,
  Plus,
  Activity,
} from 'lucide-react'
import { Header } from '@/components/layout'
import { Card, CardContent, Button, LoadingOverlay, EmptyState } from '@/components/ui'
import { SandboxCard } from '@/components/sandbox'
import { apiClient } from '@/api/client'
import type { SandboxState } from '@/types'

interface StatCardProps {
  title: string
  value: number
  icon: React.ReactNode
  color: string
  href?: string
}

function StatCard({ title, value, icon, color, href }: StatCardProps) {
  const content = (
    <Card hover={!!href} className="relative overflow-hidden">
      <div className="flex items-start justify-between">
        <div>
          <p className="text-sm font-medium text-slate-500">{title}</p>
          <p className="mt-2 text-3xl font-bold text-slate-900">{value}</p>
        </div>
        <div className={`p-3 rounded-xl ${color}`}>{icon}</div>
      </div>
      {href && (
        <div className="mt-4 flex items-center text-sm font-medium text-primary-600">
          View all
          <ArrowRight className="ml-1 h-4 w-4" />
        </div>
      )}
    </Card>
  )

  if (href) {
    return <Link to={href}>{content}</Link>
  }

  return content
}

export function Dashboard() {
  const {
    data: sandboxes,
    isLoading,
    error,
  } = useQuery({
    queryKey: ['sandboxes'],
    queryFn: () => apiClient.listSandboxes({ limit: 100 }),
  })

  const {
    data: health,
  } = useQuery({
    queryKey: ['health'],
    queryFn: () => apiClient.health(),
    retry: false,
  })

  if (isLoading) {
    return <LoadingOverlay message="Loading dashboard..." />
  }

  if (error) {
    return (
      <EmptyState
        icon={<AlertCircle className="h-8 w-8" />}
        title="Failed to load dashboard"
        description={(error as Error).message}
      />
    )
  }

  const items = sandboxes?.items || []

  const stats = {
    total: items.length,
    running: items.filter((s) => s.state === 'running').length,
    stopped: items.filter((s) => s.state === 'stopped').length,
    error: items.filter((s) => s.state === 'error').length,
  }

  const recentSandboxes = items.slice(0, 6)

  const stateFilter = (state: SandboxState) =>
    `/sandboxes?state=${state}`

  return (
    <div>
      <Header
        title="Dashboard"
        description="Overview of your sandbox environment"
        actions={
          <Link to="/sandboxes">
            <Button leftIcon={<Plus className="h-4 w-4" />}>
              Create Sandbox
            </Button>
          </Link>
        }
      />

      {/* Health Status */}
      {health && (
        <Card className="mb-6 bg-gradient-to-r from-primary-50 to-accent-50 border-primary-100">
          <div className="flex items-center gap-3">
            <div className="p-2 bg-white rounded-lg shadow-sm">
              <Activity className="h-5 w-5 text-primary-600" />
            </div>
            <div>
              <p className="text-sm font-medium text-slate-700">
                System Status: <span className="text-emerald-600">{health.status}</span>
              </p>
              <p className="text-xs text-slate-500">
                Version {health.version} &middot; Last checked: {new Date(health.timestamp).toLocaleTimeString()}
              </p>
            </div>
          </div>
        </Card>
      )}

      {/* Stats Grid */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4 mb-8">
        <StatCard
          title="Total Sandboxes"
          value={stats.total}
          icon={<Box className="h-6 w-6 text-primary-600" />}
          color="bg-primary-100"
          href="/sandboxes"
        />
        <StatCard
          title="Running"
          value={stats.running}
          icon={<Play className="h-6 w-6 text-emerald-600" />}
          color="bg-emerald-100"
          href={stateFilter('running')}
        />
        <StatCard
          title="Stopped"
          value={stats.stopped}
          icon={<Square className="h-6 w-6 text-slate-600" />}
          color="bg-slate-100"
          href={stateFilter('stopped')}
        />
        <StatCard
          title="Errors"
          value={stats.error}
          icon={<AlertCircle className="h-6 w-6 text-red-600" />}
          color="bg-red-100"
          href={stateFilter('error')}
        />
      </div>

      {/* Recent Sandboxes */}
      <div className="mb-8">
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-lg font-semibold text-slate-900">
            Recent Sandboxes
          </h2>
          <Link
            to="/sandboxes"
            className="text-sm font-medium text-primary-600 hover:text-primary-700 flex items-center gap-1"
          >
            View all
            <ArrowRight className="h-4 w-4" />
          </Link>
        </div>

        {recentSandboxes.length === 0 ? (
          <Card>
            <CardContent>
              <EmptyState
                icon={<Box className="h-8 w-8" />}
                title="No sandboxes yet"
                description="Create your first sandbox to get started"
                action={
                  <Link to="/sandboxes">
                    <Button leftIcon={<Plus className="h-4 w-4" />}>
                      Create Sandbox
                    </Button>
                  </Link>
                }
              />
            </CardContent>
          </Card>
        ) : (
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
            {recentSandboxes.map((sandbox) => (
              <SandboxCard key={sandbox.id} sandbox={sandbox} />
            ))}
          </div>
        )}
      </div>

      {/* Quick Actions */}
      <div>
        <h2 className="text-lg font-semibold text-slate-900 mb-4">
          Quick Actions
        </h2>
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
          <Link to="/sandboxes">
            <Card hover className="text-center">
              <div className="p-3 bg-primary-100 rounded-xl w-fit mx-auto mb-3">
                <Plus className="h-6 w-6 text-primary-600" />
              </div>
              <p className="font-medium text-slate-900">Create Sandbox</p>
              <p className="text-sm text-slate-500 mt-1">
                Launch a new isolated environment
              </p>
            </Card>
          </Link>
          <Link to="/sandboxes?state=running">
            <Card hover className="text-center">
              <div className="p-3 bg-emerald-100 rounded-xl w-fit mx-auto mb-3">
                <Play className="h-6 w-6 text-emerald-600" />
              </div>
              <p className="font-medium text-slate-900">Running Sandboxes</p>
              <p className="text-sm text-slate-500 mt-1">
                View and manage active sandboxes
              </p>
            </Card>
          </Link>
          <Link to="/settings">
            <Card hover className="text-center">
              <div className="p-3 bg-slate-100 rounded-xl w-fit mx-auto mb-3">
                <Activity className="h-6 w-6 text-slate-600" />
              </div>
              <p className="font-medium text-slate-900">API Settings</p>
              <p className="text-sm text-slate-500 mt-1">
                Configure your API key
              </p>
            </Card>
          </Link>
        </div>
      </div>
    </div>
  )
}
