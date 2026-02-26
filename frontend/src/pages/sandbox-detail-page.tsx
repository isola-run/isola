import { useState } from 'react'
import { useParams, useNavigate, Link } from 'react-router-dom'
import { ArrowLeft, Trash2, Copy, Globe, ShieldOff, Clock, Container } from 'lucide-react'
import { useSandbox } from '@/hooks/use-sandboxes'
import { StatusBadge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { Skeleton } from '@/components/ui/skeleton'
import { TerminalPanel } from '@/components/terminal/terminal-panel'
import { FileManager } from '@/components/filesystem/file-manager'
import { DeleteSandboxDialog } from '@/components/sandbox/delete-sandbox-dialog'
import { formatRelativeTime, formatUptime } from '@/lib/utils'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'

export function SandboxDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const { data: sandbox, isLoading, error } = useSandbox(id!)
  const [deleteOpen, setDeleteOpen] = useState(false)

  if (isLoading) {
    return (
      <div className="max-w-6xl mx-auto px-6 py-8 space-y-6 animate-fade-in">
        <Skeleton className="h-10 w-64" />
        <Skeleton className="h-64 rounded-xl" />
      </div>
    )
  }

  if (error || !sandbox) {
    return (
      <div className="max-w-6xl mx-auto px-6 py-16 text-center animate-fade-in">
        <div className="w-12 h-12 rounded-xl bg-bg-hover flex items-center justify-center mx-auto mb-4">
          <Container className="w-6 h-6 text-text-tertiary" />
        </div>
        <h2 className="text-lg font-semibold text-text-primary mb-1">Sandbox not found</h2>
        <p className="text-sm text-text-secondary mb-4">The sandbox may have been deleted.</p>
        <Link to="/sandboxes">
          <Button variant="secondary">
            <ArrowLeft className="w-4 h-4" />
            Back to sandboxes
          </Button>
        </Link>
      </div>
    )
  }

  const isRunning = sandbox.status === 'running'

  return (
    <div className="flex flex-col h-full animate-fade-in">
      {/* Header */}
      <div className="px-6 py-4 border-b border-border-subtle bg-bg-root shrink-0">
        <div className="max-w-6xl mx-auto">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-3 min-w-0">
              <Link
                to="/sandboxes"
                className="p-1.5 rounded-md text-text-tertiary hover:text-text-secondary hover:bg-bg-hover transition-colors"
              >
                <ArrowLeft className="w-4 h-4" />
              </Link>
              <div className="flex items-center gap-3 min-w-0">
                <h1 className="font-mono text-base font-semibold text-text-primary truncate">{sandbox.id}</h1>
                <button
                  onClick={() => {
                    navigator.clipboard.writeText(sandbox.id)
                    toast.success('ID copied')
                  }}
                  className="p-1 rounded text-text-tertiary hover:text-text-secondary transition-colors"
                >
                  <Copy className="w-3.5 h-3.5" />
                </button>
                <StatusBadge status={sandbox.status} />
              </div>
            </div>

            <div className="flex items-center gap-2 shrink-0">
              {isRunning && (
                <span className="text-xs font-mono text-text-secondary">
                  {formatUptime(sandbox.creationTimestamp)}
                </span>
              )}
              <Button variant="destructive" size="sm" onClick={() => setDeleteOpen(true)}>
                <Trash2 className="w-3.5 h-3.5" />
                Delete
              </Button>
            </div>
          </div>

          {/* Info bar */}
          <div className="flex items-center gap-4 mt-3 text-xs text-text-secondary">
            <span className="flex items-center gap-1.5">
              <Container className="w-3.5 h-3.5 text-text-tertiary" />
              <span className="font-mono">{sandbox.podTemplate.container.image}</span>
            </span>
            <span className="text-text-tertiary">&middot;</span>
            <span className="flex items-center gap-1.5">
              <Clock className="w-3.5 h-3.5 text-text-tertiary" />
              Created {formatRelativeTime(sandbox.creationTimestamp)}
            </span>
            {sandbox.network?.allowInternetEgress ? (
              <>
                <span className="text-text-tertiary">&middot;</span>
                <span className="flex items-center gap-1.5 text-status-running">
                  <Globe className="w-3.5 h-3.5" />
                  Internet enabled
                </span>
              </>
            ) : (
              <>
                <span className="text-text-tertiary">&middot;</span>
                <span className="flex items-center gap-1.5 text-text-tertiary">
                  <ShieldOff className="w-3.5 h-3.5" />
                  Network isolated
                </span>
              </>
            )}
            {sandbox.activeDeadlineSeconds && (
              <>
                <span className="text-text-tertiary">&middot;</span>
                <span>Timeout: {sandbox.activeDeadlineSeconds}s</span>
              </>
            )}
          </div>
        </div>
      </div>

      {/* Tab content */}
      <Tabs defaultValue={isRunning ? 'terminal' : 'overview'} className="flex-1 flex flex-col min-h-0">
        <div className="px-6 bg-bg-root shrink-0">
          <div className="max-w-6xl mx-auto">
            <TabsList>
              <TabsTrigger value="overview">Overview</TabsTrigger>
              <TabsTrigger value="terminal">Terminal</TabsTrigger>
              <TabsTrigger value="files">Files</TabsTrigger>
            </TabsList>
          </div>
        </div>

        <TabsContent value="overview" className="flex-1 overflow-y-auto">
          <div className="max-w-6xl mx-auto px-6 py-6">
            <OverviewTab sandbox={sandbox} />
          </div>
        </TabsContent>

        <TabsContent value="terminal" className="flex-1 min-h-0">
          <TerminalPanel sandboxId={sandbox.id} disabled={!isRunning} />
        </TabsContent>

        <TabsContent value="files" className="flex-1 overflow-y-auto">
          <FileManager sandboxId={sandbox.id} disabled={!isRunning} />
        </TabsContent>
      </Tabs>

      <DeleteSandboxDialog
        sandboxId={sandbox.id}
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        onDeleted={() => navigate('/sandboxes')}
      />
    </div>
  )
}

function OverviewTab({ sandbox }: { sandbox: NonNullable<ReturnType<typeof useSandbox>['data']> }) {
  return (
    <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
      {/* Container Info */}
      <div className="bg-bg-surface border border-border-subtle rounded-xl p-5">
        <h3 className="text-sm font-semibold text-text-primary mb-4">Container Configuration</h3>
        <div className="space-y-3">
          <InfoRow label="Image" value={sandbox.podTemplate.container.image} mono />
          {sandbox.podTemplate.container.command && (
            <InfoRow label="Command" value={sandbox.podTemplate.container.command.join(' ')} mono />
          )}
          {sandbox.podTemplate.container.resources?.limits && (
            <>
              {sandbox.podTemplate.container.resources.limits.cpu && (
                <InfoRow label="CPU Limit" value={sandbox.podTemplate.container.resources.limits.cpu} />
              )}
              {sandbox.podTemplate.container.resources.limits.memory && (
                <InfoRow label="Memory Limit" value={sandbox.podTemplate.container.resources.limits.memory} />
              )}
              {sandbox.podTemplate.container.resources.limits.ephemeralStorage && (
                <InfoRow label="Storage Limit" value={sandbox.podTemplate.container.resources.limits.ephemeralStorage} />
              )}
            </>
          )}
        </div>
      </div>

      {/* Network Info */}
      <div className="bg-bg-surface border border-border-subtle rounded-xl p-5">
        <h3 className="text-sm font-semibold text-text-primary mb-4">Network Configuration</h3>
        {sandbox.network ? (
          <div className="space-y-3">
            <InfoRow
              label="Internet Egress"
              value={sandbox.network.allowInternetEgress ? 'Allowed' : 'Denied'}
              valueClass={sandbox.network.allowInternetEgress ? 'text-status-running' : 'text-status-failed'}
            />
            {sandbox.network.allowClusterDNS !== undefined && (
              <InfoRow
                label="Cluster DNS"
                value={sandbox.network.allowClusterDNS ? 'Allowed' : 'Denied'}
              />
            )}
            {sandbox.network.allowedEgressCIDRs && sandbox.network.allowedEgressCIDRs.length > 0 && (
              <InfoRow label="Egress CIDRs" value={sandbox.network.allowedEgressCIDRs.join(', ')} mono />
            )}
            {sandbox.network.nameservers && sandbox.network.nameservers.length > 0 && (
              <InfoRow label="Nameservers" value={sandbox.network.nameservers.join(', ')} mono />
            )}
          </div>
        ) : (
          <p className="text-sm text-text-tertiary">Default network isolation (deny-all egress)</p>
        )}
      </div>

      {/* Lifecycle Info */}
      <div className="bg-bg-surface border border-border-subtle rounded-xl p-5">
        <h3 className="text-sm font-semibold text-text-primary mb-4">Lifecycle</h3>
        <div className="space-y-3">
          <InfoRow label="Status" value={sandbox.status} />
          <InfoRow label="Created" value={new Date(sandbox.creationTimestamp).toLocaleString()} />
          {sandbox.activeDeadlineSeconds && (
            <InfoRow label="Timeout" value={`${sandbox.activeDeadlineSeconds} seconds`} />
          )}
        </div>
      </div>

      {/* API Reference */}
      <div className="bg-bg-surface border border-border-subtle rounded-xl p-5">
        <h3 className="text-sm font-semibold text-text-primary mb-4">API Reference</h3>
        <div className="space-y-2">
          <div className="text-xs text-text-tertiary mb-2">Use these endpoints with the API:</div>
          <code className="block text-xs font-mono text-text-secondary bg-bg-inset rounded-lg px-3 py-2">
            GET /sandboxes/{sandbox.id}
          </code>
          <code className="block text-xs font-mono text-text-secondary bg-bg-inset rounded-lg px-3 py-2">
            POST /sandboxes/{sandbox.id}/commands
          </code>
          <code className="block text-xs font-mono text-text-secondary bg-bg-inset rounded-lg px-3 py-2">
            POST /sandboxes/{sandbox.id}/filesystem
          </code>
        </div>
      </div>
    </div>
  )
}

function InfoRow({
  label,
  value,
  mono,
  valueClass,
}: {
  label: string
  value: string
  mono?: boolean
  valueClass?: string
}) {
  return (
    <div className="flex items-start justify-between gap-4">
      <span className="text-xs text-text-tertiary shrink-0">{label}</span>
      <span
        className={cn(
          'text-xs text-text-secondary text-right break-all',
          mono && 'font-mono',
          valueClass,
        )}
      >
        {value}
      </span>
    </div>
  )
}
