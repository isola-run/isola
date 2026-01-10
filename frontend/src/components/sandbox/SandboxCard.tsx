import { Link } from 'react-router-dom'
import { formatDistanceToNow } from '@/lib/utils'
import { Card } from '@/components/ui'
import { SandboxStatusBadge } from './SandboxStatusBadge'
import { Clock, Tag } from 'lucide-react'
import type { Sandbox } from '@/types'

interface SandboxCardProps {
  sandbox: Sandbox
}

export function SandboxCard({ sandbox }: SandboxCardProps) {
  const labelCount = Object.keys(sandbox.labels || {}).length

  return (
    <Link to={`/sandboxes/${sandbox.id}`}>
      <Card hover className="group">
        <div className="flex items-start justify-between mb-3">
          <div className="flex-1 min-w-0">
            <h3 className="font-semibold text-slate-900 truncate group-hover:text-primary-600 transition-colors">
              {sandbox.name}
            </h3>
            <p className="text-xs text-slate-400 font-mono mt-0.5 truncate">
              {sandbox.id}
            </p>
          </div>
          <SandboxStatusBadge state={sandbox.state} />
        </div>

        <div className="flex items-center gap-4 text-xs text-slate-500">
          <div className="flex items-center gap-1.5">
            <Clock className="h-3.5 w-3.5" />
            <span>{formatDistanceToNow(sandbox.createdAt)}</span>
          </div>
          {labelCount > 0 && (
            <div className="flex items-center gap-1.5">
              <Tag className="h-3.5 w-3.5" />
              <span>{labelCount} label{labelCount !== 1 ? 's' : ''}</span>
            </div>
          )}
        </div>

        {sandbox.errorReason && (
          <p className="mt-3 text-xs text-red-600 bg-red-50 rounded-md px-2 py-1.5 truncate">
            {sandbox.errorReason}
          </p>
        )}
      </Card>
    </Link>
  )
}
