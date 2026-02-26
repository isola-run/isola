import { useEffect, useRef } from 'react'
import { useCommandStream } from '@/hooks/use-command-stream'
import { useCommandStatus } from '@/hooks/use-commands'
import { useTerminalStore } from '@/stores/terminal-store'
import { Square, RotateCcw } from 'lucide-react'
import { cn } from '@/lib/utils'

interface CommandOutputProps {
  sandboxId: string
  commandId: string
  command: string
  onKill: (cmdId: string) => void
  onRerun: (cmd: string) => void
}

export function CommandOutput({ sandboxId, commandId, command, onKill, onRerun }: CommandOutputProps) {
  const outputRef = useRef<HTMLDivElement>(null)
  const autoScrollRef = useRef(true)

  const { data: statusData } = useCommandStatus(sandboxId, commandId, true)
  const isRunning = statusData?.exitCode === null || statusData?.exitCode === undefined
  const exitCode = statusData?.exitCode

  useCommandStream(sandboxId, commandId, 'stdout', true)
  useCommandStream(sandboxId, commandId, 'stderr', true)

  const output = useTerminalStore((s) => s.outputs.get(commandId))

  // Auto-scroll to bottom
  useEffect(() => {
    if (autoScrollRef.current && outputRef.current) {
      outputRef.current.scrollTop = outputRef.current.scrollHeight
    }
  }, [output?.stdoutText, output?.stderrText])

  const handleScroll = () => {
    if (!outputRef.current) return
    const { scrollTop, scrollHeight, clientHeight } = outputRef.current
    autoScrollRef.current = scrollHeight - scrollTop - clientHeight < 50
  }

  return (
    <div className="border border-border-subtle rounded-lg overflow-hidden bg-bg-inset animate-slide-up">
      {/* Command header */}
      <div className="flex items-center justify-between px-3 py-2 bg-bg-surface border-b border-border-subtle">
        <div className="flex items-center gap-2 min-w-0">
          <span className="font-mono text-xs text-accent">$</span>
          <span className="font-mono text-xs text-text-primary truncate">{command}</span>
        </div>
        <div className="flex items-center gap-1.5 shrink-0">
          {isRunning ? (
            <>
              <span className="flex items-center gap-1.5 text-[11px] text-status-running">
                <span className="w-1.5 h-1.5 rounded-full bg-status-running animate-pulse-status" />
                Running
              </span>
              <button
                onClick={() => onKill(commandId)}
                className="p-1 rounded text-text-tertiary hover:text-error hover:bg-error/10 transition-colors"
                title="Kill command"
              >
                <Square className="w-3 h-3" />
              </button>
            </>
          ) : (
            <>
              <span
                className={cn(
                  'text-[11px] font-mono px-1.5 py-0.5 rounded',
                  exitCode === 0
                    ? 'text-text-tertiary bg-bg-hover'
                    : 'text-error bg-error/10',
                )}
              >
                exit: {exitCode}
              </span>
              <button
                onClick={() => onRerun(command)}
                className="p-1 rounded text-text-tertiary hover:text-text-secondary hover:bg-bg-hover transition-colors"
                title="Re-run command"
              >
                <RotateCcw className="w-3 h-3" />
              </button>
            </>
          )}
        </div>
      </div>

      {/* Output area */}
      <div
        ref={outputRef}
        onScroll={handleScroll}
        className="max-h-80 overflow-y-auto px-3 py-2 font-mono text-[13px] leading-5"
      >
        {/* stdout */}
        {output?.stdoutText && (
          <pre className="text-terminal-text whitespace-pre-wrap break-all">{output.stdoutText}</pre>
        )}

        {/* stderr */}
        {output?.stderrText && (
          <pre className="text-terminal-stderr whitespace-pre-wrap break-all border-l-2 border-error/30 pl-2 mt-1 bg-error/[0.03]">
            {output.stderrText}
          </pre>
        )}

        {/* Empty state when still waiting */}
        {!output?.stdoutText && !output?.stderrText && isRunning && (
          <span className="text-text-tertiary text-xs">Waiting for output...</span>
        )}
      </div>
    </div>
  )
}
