import { useLocation, Link } from 'react-router-dom';
import {
  LayoutDashboard,
  Box,
  Settings,
  FileText,
  Shield,
  Activity,
  Plus,
} from 'lucide-react';
import { cn } from '@/lib/utils';
import { Button } from '@/components/ui/Button';

interface NavItem {
  href: string;
  label: string;
  icon: React.ComponentType<{ className?: string }>;
  badge?: string;
}

const navItems: NavItem[] = [
  { href: '/', label: 'Dashboard', icon: LayoutDashboard },
  { href: '/sandboxes', label: 'Sandboxes', icon: Box },
  { href: '/templates', label: 'Templates', icon: FileText },
  { href: '/network', label: 'Network', icon: Shield },
  { href: '/activity', label: 'Activity', icon: Activity },
];

const bottomNavItems: NavItem[] = [
  { href: '/settings', label: 'Settings', icon: Settings },
];

interface SidebarProps {
  onCreateClick?: () => void;
}

function Sidebar({ onCreateClick }: SidebarProps) {
  const location = useLocation();

  return (
    <aside className="fixed inset-y-0 left-0 z-30 hidden w-64 flex-col border-r bg-background pt-14 lg:flex">
      <div className="flex flex-1 flex-col gap-2 p-4">
        {/* Create button */}
        <Button className="w-full justify-start gap-2 mb-4" onClick={onCreateClick}>
          <Plus className="h-4 w-4" />
          New Sandbox
        </Button>

        {/* Main navigation */}
        <nav className="flex flex-1 flex-col gap-1">
          {navItems.map((item) => {
            const isActive =
              location.pathname === item.href ||
              (item.href !== '/' && location.pathname.startsWith(item.href));

            return (
              <Link
                key={item.href}
                to={item.href}
                className={cn(
                  'flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm font-medium transition-colors',
                  isActive
                    ? 'bg-primary/10 text-primary'
                    : 'text-muted-foreground hover:bg-muted hover:text-foreground'
                )}
              >
                <item.icon className="h-4 w-4" />
                {item.label}
                {item.badge && (
                  <span className="ml-auto rounded-full bg-primary/10 px-2 py-0.5 text-xs text-primary">
                    {item.badge}
                  </span>
                )}
              </Link>
            );
          })}
        </nav>

        {/* Bottom navigation */}
        <nav className="flex flex-col gap-1 border-t pt-4">
          {bottomNavItems.map((item) => {
            const isActive = location.pathname === item.href;

            return (
              <Link
                key={item.href}
                to={item.href}
                className={cn(
                  'flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm font-medium transition-colors',
                  isActive
                    ? 'bg-primary/10 text-primary'
                    : 'text-muted-foreground hover:bg-muted hover:text-foreground'
                )}
              >
                <item.icon className="h-4 w-4" />
                {item.label}
              </Link>
            );
          })}
        </nav>

        {/* Version info */}
        <div className="mt-auto text-xs text-muted-foreground px-3 py-2">
          Isola v0.1.0
        </div>
      </div>
    </aside>
  );
}

export { Sidebar };
