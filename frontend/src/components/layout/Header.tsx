import type { ReactNode } from 'react'
import { clsx } from 'clsx'

interface HeaderProps {
  title: string
  description?: ReactNode
  actions?: ReactNode
  className?: string
}

export function Header({ title, description, actions, className }: HeaderProps) {
  return (
    <header
      className={clsx(
        'flex flex-col sm:flex-row sm:items-center justify-between gap-4 mb-8',
        className
      )}
    >
      <div>
        <h1 className="text-2xl font-bold text-slate-900">{title}</h1>
        {description && (
          <div className="mt-1 text-sm text-slate-500">{description}</div>
        )}
      </div>
      {actions && <div className="flex items-center gap-3">{actions}</div>}
    </header>
  )
}
