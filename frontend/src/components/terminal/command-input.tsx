import { useState, useRef, useCallback } from 'react'
import { Play, Loader2 } from 'lucide-react'
import { cn } from '@/lib/utils'

interface CommandInputProps {
  onSubmit: (cmd: string, args: string[]) => void
  disabled?: boolean
  isLoading?: boolean
}

export function CommandInput({ onSubmit, disabled, isLoading }: CommandInputProps) {
  const [value, setValue] = useState('')
  const [history, setHistory] = useState<string[]>([])
  const [historyIndex, setHistoryIndex] = useState(-1)
  const inputRef = useRef<HTMLInputElement>(null)

  const handleSubmit = useCallback(() => {
    const trimmed = value.trim()
    if (!trimmed) return

    // Parse command: first word is cmd, rest are args
    const parts = trimmed.split(/\s+/)
    const cmd = parts[0]
    const args = parts.slice(1)

    onSubmit(cmd, args)
    setHistory((prev) => [trimmed, ...prev])
    setValue('')
    setHistoryIndex(-1)
  }, [value, onSubmit])

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      handleSubmit()
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      if (historyIndex < history.length - 1) {
        const newIndex = historyIndex + 1
        setHistoryIndex(newIndex)
        setValue(history[newIndex])
      }
    } else if (e.key === 'ArrowDown') {
      e.preventDefault()
      if (historyIndex > 0) {
        const newIndex = historyIndex - 1
        setHistoryIndex(newIndex)
        setValue(history[newIndex])
      } else if (historyIndex === 0) {
        setHistoryIndex(-1)
        setValue('')
      }
    }
  }

  return (
    <div className="flex items-center gap-2 px-4 py-3 bg-terminal-bg border-t border-border-subtle">
      <span className="text-accent font-mono text-sm font-medium select-none">$</span>
      <input
        ref={inputRef}
        value={value}
        onChange={(e) => setValue(e.target.value)}
        onKeyDown={handleKeyDown}
        disabled={disabled}
        placeholder={disabled ? 'Sandbox not ready...' : 'Enter command...'}
        className={cn(
          'flex-1 bg-transparent text-terminal-text font-mono text-sm outline-none',
          'placeholder:text-text-tertiary',
          'disabled:opacity-50 disabled:cursor-not-allowed',
        )}
        autoComplete="off"
        spellCheck={false}
      />
      <button
        onClick={handleSubmit}
        disabled={disabled || !value.trim() || isLoading}
        className={cn(
          'p-1.5 rounded-md transition-colors',
          'text-text-tertiary hover:text-accent hover:bg-accent/10',
          'disabled:opacity-30 disabled:pointer-events-none',
        )}
      >
        {isLoading ? (
          <Loader2 className="w-4 h-4 animate-spin" />
        ) : (
          <Play className="w-4 h-4" />
        )}
      </button>
    </div>
  )
}
