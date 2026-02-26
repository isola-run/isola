import { Link } from 'react-router-dom'
import { MoreHorizontal, Trash2, ExternalLink, Copy } from 'lucide-react'
import { StatusBadge } from '@/components/ui/badge'
import { DropdownMenu, DropdownMenuTrigger, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator } from '@/components/ui/dropdown-menu'
import { formatRelativeTime } from '@/lib/utils'
import type { SandboxSummary } from '@/api/types'
import { toast } from 'sonner'

interface SandboxCardProps {
  sandbox: SandboxSummary
  onDelete: (id: string) => void
}

export function SandboxCard({ sandbox, onDelete }: SandboxCardProps) {
  return (
    <Link
      to={`/sandboxes/${sandbox.id}`}
      className="group block bg-bg-surface border border-border-subtle rounded-xl p-4 hover:border-border-default hover:bg-bg-hover/50 transition-all duration-150"
    >
      <div className="flex items-start justify-between mb-3">
        <div className="flex items-center gap-2.5 min-w-0">
          <StatusBadge status={sandbox.status} size="sm" />
        </div>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <button
              onClick={(e) => e.preventDefault()}
              className="p-1 rounded-md text-text-tertiary hover:text-text-secondary hover:bg-bg-active transition-colors opacity-0 group-hover:opacity-100"
            >
              <MoreHorizontal className="w-4 h-4" />
            </button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" onClick={(e) => e.preventDefault()}>
            <DropdownMenuItem onSelect={() => {
              navigator.clipboard.writeText(sandbox.id)
              toast.success('Sandbox ID copied')
            }}>
              <Copy className="w-3.5 h-3.5" />
              Copy ID
            </DropdownMenuItem>
            <DropdownMenuItem onSelect={() => window.open(`/sandboxes/${sandbox.id}`, '_blank')}>
              <ExternalLink className="w-3.5 h-3.5" />
              Open in new tab
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem destructive onSelect={() => onDelete(sandbox.id)}>
              <Trash2 className="w-3.5 h-3.5" />
              Delete
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>

      <div className="space-y-2">
        <h3 className="font-mono text-sm font-medium text-text-primary truncate">
          {sandbox.id}
        </h3>
        <div className="flex items-center gap-3 text-xs text-text-secondary">
          <span className="shrink-0">{formatRelativeTime(sandbox.creationTimestamp)}</span>
        </div>
      </div>
    </Link>
  )
}
