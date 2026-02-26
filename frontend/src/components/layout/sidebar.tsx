import { Link, useLocation } from 'react-router-dom'
import { LayoutDashboard, Box, Settings, Plus, PanelLeftClose, PanelLeft } from 'lucide-react'
import { cn } from '@/lib/utils'
import { useLayoutStore } from '@/stores/layout-store'
import { useSandboxes } from '@/hooks/use-sandboxes'
import { StatusDot } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'

const navItems = [
  { path: '/', label: 'Dashboard', icon: LayoutDashboard },
  { path: '/sandboxes', label: 'Sandboxes', icon: Box },
  { path: '/settings', label: 'Settings', icon: Settings },
]

export function Sidebar() {
  const location = useLocation()
  const { sidebarCollapsed, toggleSidebar } = useLayoutStore()
  const { data: sandboxes } = useSandboxes()

  const runningSandboxes = sandboxes?.filter((s) => s.status === 'running' || s.status === 'creating') ?? []

  return (
    <aside
      className={cn(
        'h-screen flex flex-col bg-bg-surface border-r border-border-subtle transition-all duration-200 ease-out shrink-0',
        sidebarCollapsed ? 'w-16' : 'w-60',
      )}
    >
      {/* Logo */}
      <div className="h-14 flex items-center px-4 border-b border-border-subtle shrink-0">
        <Link to="/" className="flex items-center gap-2.5 group">
          <div className="w-7 h-7 rounded-lg bg-accent/10 border border-accent/20 flex items-center justify-center shrink-0">
            <div className="w-3 h-3 rounded-sm bg-accent" />
          </div>
          {!sidebarCollapsed && (
            <span className="text-base font-semibold tracking-tight text-text-primary">
              <span className="text-accent">I</span>sola
            </span>
          )}
        </Link>
      </div>

      {/* Navigation */}
      <nav className="flex-1 px-2 py-3 space-y-1 overflow-y-auto">
        {navItems.map((item) => {
          const isActive =
            item.path === '/'
              ? location.pathname === '/'
              : location.pathname.startsWith(item.path)
          return (
            <Link
              key={item.path}
              to={item.path}
              className={cn(
                'flex items-center gap-3 px-3 h-9 rounded-lg text-sm font-medium transition-colors duration-150',
                isActive
                  ? 'bg-bg-active text-text-primary border-l-2 border-accent'
                  : 'text-text-secondary hover:text-text-primary hover:bg-bg-hover',
                sidebarCollapsed && 'justify-center px-0',
              )}
            >
              <item.icon className="w-4 h-4 shrink-0" />
              {!sidebarCollapsed && item.label}
            </Link>
          )
        })}

        {/* Running sandboxes quick access */}
        {!sidebarCollapsed && runningSandboxes.length > 0 && (
          <div className="pt-4">
            <div className="px-3 pb-2 text-[11px] font-medium text-text-tertiary uppercase tracking-wider">
              Active
            </div>
            {runningSandboxes.slice(0, 5).map((sb) => (
              <Link
                key={sb.id}
                to={`/sandboxes/${sb.id}`}
                className={cn(
                  'flex items-center gap-2.5 px-3 h-8 rounded-lg text-sm transition-colors duration-150',
                  location.pathname === `/sandboxes/${sb.id}`
                    ? 'text-text-primary bg-bg-hover'
                    : 'text-text-secondary hover:text-text-primary hover:bg-bg-hover',
                )}
              >
                <StatusDot status={sb.status} />
                <span className="truncate font-mono text-xs">{sb.id}</span>
              </Link>
            ))}
          </div>
        )}
      </nav>

      {/* Bottom section */}
      <div className="px-2 py-3 border-t border-border-subtle space-y-2 shrink-0">
        {!sidebarCollapsed ? (
          <Link to="/sandboxes?create=true">
            <Button variant="primary" size="sm" className="w-full">
              <Plus className="w-3.5 h-3.5" />
              New Sandbox
            </Button>
          </Link>
        ) : (
          <Link to="/sandboxes?create=true" className="flex justify-center">
            <Button variant="primary" size="sm" className="w-9 h-9 p-0">
              <Plus className="w-4 h-4" />
            </Button>
          </Link>
        )}
        <button
          onClick={toggleSidebar}
          className="flex items-center justify-center w-full h-8 text-text-tertiary hover:text-text-secondary transition-colors rounded-lg hover:bg-bg-hover"
        >
          {sidebarCollapsed ? (
            <PanelLeft className="w-4 h-4" />
          ) : (
            <PanelLeftClose className="w-4 h-4" />
          )}
        </button>
      </div>
    </aside>
  )
}
