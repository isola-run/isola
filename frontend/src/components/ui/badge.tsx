import type { SandboxStatus } from '@/api/types'
import { cn } from '@/lib/utils'

const statusConfig: Record<SandboxStatus, { label: string; dotClass: string; bgClass: string; textClass: string }> = {
  creating: {
    label: 'Creating',
    dotClass: 'bg-status-creating animate-pulse-status',
    bgClass: 'bg-status-creating/15',
    textClass: 'text-status-creating',
  },
  running: {
    label: 'Running',
    dotClass: 'bg-status-running shadow-[0_0_8px_rgba(34,197,94,0.4)]',
    bgClass: 'bg-status-running/15',
    textClass: 'text-status-running',
  },
  shuttingDown: {
    label: 'Shutting Down',
    dotClass: 'bg-status-shutting-down animate-pulse',
    bgClass: 'bg-status-shutting-down/15',
    textClass: 'text-status-shutting-down',
  },
  failed: {
    label: 'Failed',
    dotClass: 'bg-status-failed',
    bgClass: 'bg-status-failed/15',
    textClass: 'text-status-failed',
  },
  stopped: {
    label: 'Stopped',
    dotClass: 'bg-status-stopped',
    bgClass: 'bg-status-stopped/15',
    textClass: 'text-status-stopped',
  },
  unknown: {
    label: 'Unknown',
    dotClass: 'bg-status-unknown',
    bgClass: 'bg-status-unknown/15',
    textClass: 'text-status-unknown',
  },
}

export function StatusBadge({ status, size = 'md' }: { status: SandboxStatus; size?: 'sm' | 'md' }) {
  const config = statusConfig[status]
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 font-medium rounded-full',
        config.bgClass,
        config.textClass,
        size === 'sm' ? 'px-2 py-0.5 text-[11px]' : 'px-2.5 py-1 text-xs',
      )}
    >
      <span className={cn('rounded-full shrink-0', config.dotClass, size === 'sm' ? 'w-1.5 h-1.5' : 'w-2 h-2')} />
      {config.label}
    </span>
  )
}

export function StatusDot({ status }: { status: SandboxStatus }) {
  const config = statusConfig[status]
  return <span className={cn('w-2 h-2 rounded-full shrink-0', config.dotClass)} />
}
