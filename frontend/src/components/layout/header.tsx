import { useLocation, Link } from 'react-router-dom'
import { ChevronRight, Search } from 'lucide-react'
import { cn } from '@/lib/utils'

function getBreadcrumbs(pathname: string): Array<{ label: string; path: string }> {
  const crumbs: Array<{ label: string; path: string }> = []

  if (pathname === '/') {
    crumbs.push({ label: 'Dashboard', path: '/' })
  } else if (pathname.startsWith('/sandboxes')) {
    crumbs.push({ label: 'Sandboxes', path: '/sandboxes' })
    const parts = pathname.split('/').filter(Boolean)
    if (parts.length > 1) {
      crumbs.push({ label: parts[1], path: `/sandboxes/${parts[1]}` })
    }
  } else if (pathname === '/settings') {
    crumbs.push({ label: 'Settings', path: '/settings' })
  }

  return crumbs
}

export function Header() {
  const location = useLocation()
  const breadcrumbs = getBreadcrumbs(location.pathname)

  return (
    <header className="h-14 flex items-center justify-between px-6 border-b border-border-subtle bg-bg-root/80 backdrop-blur-md shrink-0">
      {/* Breadcrumbs */}
      <nav className="flex items-center gap-1.5">
        {breadcrumbs.map((crumb, i) => (
          <span key={crumb.path} className="flex items-center gap-1.5">
            {i > 0 && <ChevronRight className="w-3.5 h-3.5 text-text-tertiary" />}
            {i === breadcrumbs.length - 1 ? (
              <span
                className={cn(
                  'text-sm font-medium',
                  i === 0 && breadcrumbs.length === 1 ? 'text-text-primary' : 'text-text-primary',
                  breadcrumbs.length > 1 && i === breadcrumbs.length - 1 && 'font-mono text-xs',
                )}
              >
                {crumb.label}
              </span>
            ) : (
              <Link
                to={crumb.path}
                className="text-sm font-medium text-text-secondary hover:text-text-primary transition-colors"
              >
                {crumb.label}
              </Link>
            )}
          </span>
        ))}
      </nav>

      {/* Search hint */}
      <div className="flex items-center gap-3">
        <button className="flex items-center gap-2 px-3 h-8 rounded-lg bg-bg-hover/50 border border-border-subtle text-text-tertiary hover:text-text-secondary hover:border-border-default transition-all text-sm">
          <Search className="w-3.5 h-3.5" />
          <span className="hidden sm:inline">Search...</span>
          <kbd className="hidden sm:inline-flex items-center gap-0.5 px-1.5 h-5 text-[10px] font-medium bg-bg-surface rounded border border-border-default">
            <span className="text-xs">&#8984;</span>K
          </kbd>
        </button>
      </div>
    </header>
  )
}
