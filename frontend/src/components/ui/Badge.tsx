import { type HTMLAttributes } from 'react';
import { cn } from '@/lib/utils';
import type { SandboxState } from '@/types/sandbox';

interface BadgeProps extends HTMLAttributes<HTMLDivElement> {
  variant?: 'default' | 'secondary' | 'destructive' | 'outline' | 'success' | 'warning';
}

function Badge({ className, variant = 'default', ...props }: BadgeProps) {
  const variants = {
    default: 'bg-primary text-primary-foreground hover:bg-primary/80',
    secondary: 'bg-secondary text-secondary-foreground hover:bg-secondary/80',
    destructive: 'bg-destructive text-destructive-foreground hover:bg-destructive/80',
    outline: 'text-foreground border border-input',
    success: 'bg-emerald-100 text-emerald-800 dark:bg-emerald-900/30 dark:text-emerald-400',
    warning: 'bg-amber-100 text-amber-800 dark:bg-amber-900/30 dark:text-amber-400',
  };

  return (
    <div
      className={cn(
        'inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-semibold transition-colors focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2',
        variants[variant],
        className
      )}
      {...props}
    />
  );
}

const stateConfig: Record<SandboxState, { variant: BadgeProps['variant']; label: string }> = {
  pending: { variant: 'warning', label: 'Pending' },
  starting: { variant: 'warning', label: 'Starting' },
  running: { variant: 'success', label: 'Running' },
  terminating: { variant: 'secondary', label: 'Terminating' },
  stopped: { variant: 'secondary', label: 'Stopped' },
  error: { variant: 'destructive', label: 'Error' },
  unknown: { variant: 'outline', label: 'Unknown' },
};

interface StateBadgeProps extends Omit<HTMLAttributes<HTMLDivElement>, 'children'> {
  state: SandboxState;
}

function StateBadge({ state, className, ...props }: StateBadgeProps) {
  const config = stateConfig[state] || stateConfig.unknown;

  return (
    <Badge variant={config.variant} className={cn('gap-1.5', className)} {...props}>
      <span
        className={cn('h-1.5 w-1.5 rounded-full', {
          'bg-emerald-500 animate-pulse': state === 'running',
          'bg-amber-500 animate-pulse-subtle': state === 'pending' || state === 'starting',
          'bg-gray-400': state === 'stopped' || state === 'terminating',
          'bg-red-500': state === 'error',
          'bg-gray-300': state === 'unknown',
        })}
      />
      {config.label}
    </Badge>
  );
}

export { Badge, StateBadge };
