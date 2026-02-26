import { forwardRef, type ButtonHTMLAttributes } from 'react'
import { cn } from '@/lib/utils'

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: 'primary' | 'secondary' | 'destructive' | 'ghost'
  size?: 'sm' | 'md' | 'lg'
}

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant = 'primary', size = 'md', disabled, ...props }, ref) => {
    return (
      <button
        ref={ref}
        disabled={disabled}
        className={cn(
          'inline-flex items-center justify-center font-medium transition-all duration-150 ease-out',
          'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/50 focus-visible:ring-offset-2 focus-visible:ring-offset-bg-root',
          'disabled:opacity-50 disabled:pointer-events-none',
          {
            'bg-accent text-white hover:bg-accent-hover hover:-translate-y-px active:translate-y-0 shadow-sm':
              variant === 'primary',
            'border border-border-default text-text-secondary hover:bg-bg-hover hover:text-text-primary':
              variant === 'secondary',
            'border border-border-default text-text-secondary hover:bg-error/10 hover:border-error/30 hover:text-error':
              variant === 'destructive',
            'text-text-secondary hover:text-text-primary hover:bg-bg-hover':
              variant === 'ghost',
          },
          {
            'h-7 px-2.5 text-xs rounded-md gap-1.5': size === 'sm',
            'h-9 px-4 text-sm rounded-lg gap-2': size === 'md',
            'h-11 px-6 text-sm rounded-lg gap-2': size === 'lg',
          },
          className,
        )}
        {...props}
      />
    )
  },
)
Button.displayName = 'Button'
