import { useState, useRef, useEffect } from 'react'
import { clsx } from 'clsx'
import { Send, Terminal as TerminalIcon, Loader2 } from 'lucide-react'
import { Button } from '@/components/ui'
import { apiClient } from '@/api/client'
import type { ExecuteCommandResponse } from '@/types'

interface TerminalProps {
  sandboxId: string
  disabled?: boolean
}

interface HistoryEntry {
  command: string
  result?: ExecuteCommandResponse
  error?: string
  isLoading?: boolean
}

export function Terminal({ sandboxId, disabled = false }: TerminalProps) {
  const [command, setCommand] = useState('')
  const [history, setHistory] = useState<HistoryEntry[]>([])
  const [commandHistory, setCommandHistory] = useState<string[]>([])
  const [historyIndex, setHistoryIndex] = useState(-1)
  const outputRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (outputRef.current) {
      outputRef.current.scrollTop = outputRef.current.scrollHeight
    }
  }, [history])

  const executeCommand = async () => {
    if (!command.trim() || disabled) return

    const trimmedCommand = command.trim()
    setCommand('')
    setCommandHistory((prev) => [...prev, trimmedCommand])
    setHistoryIndex(-1)

    const entryIndex = history.length
    setHistory((prev) => [...prev, { command: trimmedCommand, isLoading: true }])

    try {
      const result = await apiClient.executeCommand(sandboxId, trimmedCommand)
      setHistory((prev) =>
        prev.map((entry, i) =>
          i === entryIndex ? { ...entry, result, isLoading: false } : entry
        )
      )
    } catch (err) {
      setHistory((prev) =>
        prev.map((entry, i) =>
          i === entryIndex
            ? { ...entry, error: (err as Error).message, isLoading: false }
            : entry
        )
      )
    }
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') {
      e.preventDefault()
      executeCommand()
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      if (commandHistory.length > 0) {
        const newIndex =
          historyIndex < commandHistory.length - 1
            ? historyIndex + 1
            : historyIndex
        setHistoryIndex(newIndex)
        setCommand(commandHistory[commandHistory.length - 1 - newIndex] || '')
      }
    } else if (e.key === 'ArrowDown') {
      e.preventDefault()
      if (historyIndex > 0) {
        const newIndex = historyIndex - 1
        setHistoryIndex(newIndex)
        setCommand(commandHistory[commandHistory.length - 1 - newIndex] || '')
      } else if (historyIndex === 0) {
        setHistoryIndex(-1)
        setCommand('')
      }
    }
  }

  return (
    <div className="terminal flex flex-col h-full">
      {/* Header */}
      <div className="flex items-center gap-2 px-4 py-2 bg-slate-800 border-b border-slate-700">
        <TerminalIcon className="h-4 w-4 text-slate-400" />
        <span className="text-sm font-medium text-slate-300">Terminal</span>
        {disabled && (
          <span className="ml-auto text-xs text-amber-400">
            Sandbox not running
          </span>
        )}
      </div>

      {/* Output */}
      <div
        ref={outputRef}
        className="flex-1 overflow-y-auto p-4 space-y-3 min-h-[200px] max-h-[400px]"
        onClick={() => inputRef.current?.focus()}
      >
        {history.length === 0 && (
          <p className="text-slate-500 text-sm">
            Type a command and press Enter to execute...
          </p>
        )}
        {history.map((entry, i) => (
          <div key={i} className="terminal-output">
            <div className="flex items-center gap-2 text-primary-400 mb-1">
              <span className="text-slate-500">$</span>
              <span>{entry.command}</span>
              {entry.isLoading && (
                <Loader2 className="h-3 w-3 animate-spin text-slate-400" />
              )}
            </div>
            {entry.result && (
              <>
                {entry.result.stdout && (
                  <pre className="stdout whitespace-pre-wrap text-slate-200">
                    {entry.result.stdout}
                  </pre>
                )}
                {entry.result.stderr && (
                  <pre className="stderr whitespace-pre-wrap text-red-400">
                    {entry.result.stderr}
                  </pre>
                )}
                {entry.result.exitCode !== 0 && (
                  <div className="text-amber-400 text-xs mt-1">
                    Exit code: {entry.result.exitCode}
                  </div>
                )}
              </>
            )}
            {entry.error && (
              <div className="text-red-400 text-sm">{entry.error}</div>
            )}
          </div>
        ))}
      </div>

      {/* Input */}
      <div className="flex items-center gap-2 px-4 py-3 bg-slate-800 border-t border-slate-700">
        <span className="text-slate-500">$</span>
        <input
          ref={inputRef}
          type="text"
          value={command}
          onChange={(e) => setCommand(e.target.value)}
          onKeyDown={handleKeyDown}
          disabled={disabled}
          placeholder={disabled ? 'Start sandbox to execute commands' : 'Enter command...'}
          className={clsx(
            'flex-1 bg-transparent text-slate-100 outline-none font-mono text-sm placeholder:text-slate-600',
            disabled && 'cursor-not-allowed'
          )}
        />
        <Button
          size="sm"
          variant="ghost"
          onClick={executeCommand}
          disabled={disabled || !command.trim()}
          className="text-slate-400 hover:text-white"
        >
          <Send className="h-4 w-4" />
        </Button>
      </div>
    </div>
  )
}
