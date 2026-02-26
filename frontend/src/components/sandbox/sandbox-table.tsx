import { Link } from 'react-router-dom'
import { MoreHorizontal, Trash2, Copy } from 'lucide-react'
import { StatusBadge } from '@/components/ui/badge'
import { DropdownMenu, DropdownMenuTrigger, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator } from '@/components/ui/dropdown-menu'
import { formatRelativeTime, formatUptime } from '@/lib/utils'
import type { SandboxResponse } from '@/api/types'
import { toast } from 'sonner'

interface SandboxTableProps {
  sandboxes: SandboxResponse[]
  onDelete: (id: string) => void
}

export function SandboxTable({ sandboxes, onDelete }: SandboxTableProps) {
  return (
    <div className="overflow-x-auto">
      <table className="w-full">
        <thead>
          <tr className="border-b border-border-subtle">
            <th className="text-left py-2.5 px-4 text-xs font-medium text-text-tertiary uppercase tracking-wider">Status</th>
            <th className="text-left py-2.5 px-4 text-xs font-medium text-text-tertiary uppercase tracking-wider">Name</th>
            <th className="text-left py-2.5 px-4 text-xs font-medium text-text-tertiary uppercase tracking-wider">Image</th>
            <th className="text-left py-2.5 px-4 text-xs font-medium text-text-tertiary uppercase tracking-wider">Created</th>
            <th className="text-left py-2.5 px-4 text-xs font-medium text-text-tertiary uppercase tracking-wider">Uptime</th>
            <th className="w-10"></th>
          </tr>
        </thead>
        <tbody>
          {sandboxes.map((sandbox) => (
            <tr
              key={sandbox.id}
              className="border-b border-border-subtle/50 hover:bg-bg-hover/50 transition-colors group"
            >
              <td className="py-3 px-4">
                <StatusBadge status={sandbox.status} size="sm" />
              </td>
              <td className="py-3 px-4">
                <Link
                  to={`/sandboxes/${sandbox.id}`}
                  className="font-mono text-sm text-text-primary hover:text-accent transition-colors"
                >
                  {sandbox.id}
                </Link>
              </td>
              <td className="py-3 px-4">
                <span className="text-sm text-text-secondary truncate max-w-[200px] block">
                  {sandbox.podTemplate.container.image}
                </span>
              </td>
              <td className="py-3 px-4">
                <span className="text-sm text-text-secondary">
                  {formatRelativeTime(sandbox.creationTimestamp)}
                </span>
              </td>
              <td className="py-3 px-4">
                <span className="text-sm text-text-secondary font-mono">
                  {sandbox.status === 'running' ? formatUptime(sandbox.creationTimestamp) : '—'}
                </span>
              </td>
              <td className="py-3 px-2">
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <button className="p-1.5 rounded-md text-text-tertiary hover:text-text-secondary hover:bg-bg-active transition-colors opacity-0 group-hover:opacity-100">
                      <MoreHorizontal className="w-4 h-4" />
                    </button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end">
                    <DropdownMenuItem onSelect={() => {
                      navigator.clipboard.writeText(sandbox.id)
                      toast.success('Sandbox ID copied')
                    }}>
                      <Copy className="w-3.5 h-3.5" />
                      Copy ID
                    </DropdownMenuItem>
                    <DropdownMenuSeparator />
                    <DropdownMenuItem destructive onSelect={() => onDelete(sandbox.id)}>
                      <Trash2 className="w-3.5 h-3.5" />
                      Delete
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
