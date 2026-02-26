import { useState, useCallback } from 'react'
import { CommandInput } from './command-input'
import { CommandOutput } from './command-output'
import { useCreateCommand, useKillCommand } from '@/hooks/use-commands'
import { Terminal as TerminalIcon } from 'lucide-react'
import { toast } from 'sonner'

interface CommandEntry {
  commandId: string
  command: string
}

interface TerminalPanelProps {
  sandboxId: string
  disabled?: boolean
}

export function TerminalPanel({ sandboxId, disabled }: TerminalPanelProps) {
  const [commands, setCommands] = useState<CommandEntry[]>([])
  const createCommand = useCreateCommand(sandboxId)
  const killCommand = useKillCommand(sandboxId)

  const handleSubmit = useCallback(async (cmd: string, args: string[]) => {
    try {
      const result = await createCommand.mutateAsync({ cmd, args })
      setCommands((prev) => [...prev, { commandId: result.commandId, command: [cmd, ...args].join(' ') }])
    } catch (err) {
      toast.error(`Failed to execute command: ${err instanceof Error ? err.message : 'Unknown error'}`)
    }
  }, [createCommand])

  const handleKill = useCallback(async (cmdId: string) => {
    try {
      await killCommand.mutateAsync(cmdId)
      toast.success('Command killed')
    } catch (err) {
      toast.error(`Failed to kill command: ${err instanceof Error ? err.message : 'Unknown error'}`)
    }
  }, [killCommand])

  const handleRerun = useCallback((cmd: string) => {
    const parts = cmd.split(/\s+/)
    handleSubmit(parts[0], parts.slice(1))
  }, [handleSubmit])

  return (
    <div className="flex flex-col h-full">
      {/* Command output area */}
      <div className="flex-1 overflow-y-auto p-4 space-y-3">
        {commands.length === 0 ? (
          <div className="flex flex-col items-center justify-center h-full text-center py-12">
            <div className="w-10 h-10 rounded-lg bg-bg-hover flex items-center justify-center mb-3">
              <TerminalIcon className="w-5 h-5 text-text-tertiary" />
            </div>
            <p className="text-sm text-text-secondary mb-1">No commands yet</p>
            <p className="text-xs text-text-tertiary">
              Run your first command below. Try: <span className="font-mono text-accent/70">ls /</span> or <span className="font-mono text-accent/70">uname -a</span>
            </p>
          </div>
        ) : (
          commands.map((entry) => (
            <CommandOutput
              key={entry.commandId}
              sandboxId={sandboxId}
              commandId={entry.commandId}
              command={entry.command}
              onKill={handleKill}
              onRerun={handleRerun}
            />
          ))
        )}
      </div>

      {/* Command input */}
      <CommandInput
        onSubmit={handleSubmit}
        disabled={disabled}
        isLoading={createCommand.isPending}
      />
    </div>
  )
}
