import { Moon, Sun, Monitor, Bell, Settings, HelpCircle } from 'lucide-react';
import { Button } from '@/components/ui/Button';
import { useTheme } from '@/hooks/useTheme';
import { useHealth } from '@/hooks/useSandboxes';
import { cn } from '@/lib/utils';

function Header() {
  const { theme, setTheme } = useTheme();
  const { data: health } = useHealth();

  const cycleTheme = () => {
    const themes: Array<'light' | 'dark' | 'system'> = ['light', 'dark', 'system'];
    const currentIndex = themes.indexOf(theme);
    const nextIndex = (currentIndex + 1) % themes.length;
    setTheme(themes[nextIndex]);
  };

  const themeIcon = {
    light: Sun,
    dark: Moon,
    system: Monitor,
  }[theme];

  const ThemeIcon = themeIcon;

  return (
    <header className="sticky top-0 z-40 w-full border-b bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/60">
      <div className="flex h-14 items-center justify-between px-6">
        {/* Left side - Brand */}
        <div className="flex items-center gap-4">
          <a href="/" className="flex items-center gap-2.5">
            <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-gradient-to-br from-indigo-500 to-purple-600 shadow-lg shadow-indigo-500/25">
              <svg
                viewBox="0 0 24 24"
                fill="none"
                className="h-5 w-5 text-white"
                stroke="currentColor"
                strokeWidth="2"
                strokeLinecap="round"
              >
                <rect x="4" y="8" width="16" height="4" rx="1" />
                <rect x="4" y="14" width="12" height="3" rx="1" />
                <circle cx="19" cy="15.5" r="1.5" fill="currentColor" />
              </svg>
            </div>
            <span className="text-lg font-semibold tracking-tight">Isola</span>
          </a>

          {/* Status indicator */}
          <div className="hidden sm:flex items-center gap-2 text-xs text-muted-foreground">
            <span
              className={cn(
                'h-2 w-2 rounded-full',
                health?.status === 'healthy'
                  ? 'bg-emerald-500'
                  : 'bg-amber-500 animate-pulse'
              )}
            />
            <span>
              {health?.status === 'healthy' ? 'All systems operational' : 'Connecting...'}
            </span>
          </div>
        </div>

        {/* Right side - Actions */}
        <div className="flex items-center gap-1">
          <Button variant="ghost" size="icon" className="h-9 w-9" title="Notifications">
            <Bell className="h-4 w-4" />
          </Button>

          <Button variant="ghost" size="icon" className="h-9 w-9" title="Help">
            <HelpCircle className="h-4 w-4" />
          </Button>

          <Button
            variant="ghost"
            size="icon"
            className="h-9 w-9"
            onClick={cycleTheme}
            title={`Theme: ${theme}`}
          >
            <ThemeIcon className="h-4 w-4" />
          </Button>

          <a href="/settings">
            <Button variant="ghost" size="icon" className="h-9 w-9" title="Settings">
              <Settings className="h-4 w-4" />
            </Button>
          </a>
        </div>
      </div>
    </header>
  );
}

export { Header };
