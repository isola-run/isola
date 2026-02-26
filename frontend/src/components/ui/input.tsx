import { forwardRef, type InputHTMLAttributes, type TextareaHTMLAttributes } from 'react'
import { cn } from '@/lib/utils'

export const Input = forwardRef<HTMLInputElement, InputHTMLAttributes<HTMLInputElement>>(
  ({ className, ...props }, ref) => {
    return (
      <input
        ref={ref}
        className={cn(
          'w-full h-9 px-3 text-sm text-text-primary bg-bg-input border border-border-default rounded-lg',
          'placeholder:text-text-tertiary',
          'focus:outline-none focus:border-accent focus:ring-1 focus:ring-accent/20 focus:shadow-glow',
          'transition-all duration-150',
          'disabled:opacity-50 disabled:cursor-not-allowed',
          className,
        )}
        {...props}
      />
    )
  },
)
Input.displayName = 'Input'

export const Textarea = forwardRef<HTMLTextAreaElement, TextareaHTMLAttributes<HTMLTextAreaElement>>(
  ({ className, ...props }, ref) => {
    return (
      <textarea
        ref={ref}
        className={cn(
          'w-full px-3 py-2 text-sm text-text-primary bg-bg-input border border-border-default rounded-lg',
          'placeholder:text-text-tertiary',
          'focus:outline-none focus:border-accent focus:ring-1 focus:ring-accent/20',
          'transition-all duration-150 resize-none',
          className,
        )}
        {...props}
      />
    )
  },
)
Textarea.displayName = 'Textarea'

export function Label({ children, className, ...props }: React.LabelHTMLAttributes<HTMLLabelElement>) {
  return (
    <label className={cn('text-sm font-medium text-text-secondary', className)} {...props}>
      {children}
    </label>
  )
}
