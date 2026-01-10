import { useState, useRef, useEffect } from 'react';
import { Send, Loader2, AlertCircle, Copy, Check, Trash2 } from 'lucide-react';
import { Button } from '@/components/ui/Button';
import { useExecuteCommand } from '@/hooks/useSandboxes';

interface SandboxTerminalProps {
  sandboxId: string;
}

interface CommandOutput {
  id: string;
  command: string;
  stdout: string;
  stderr: string;
  exitCode: number;
  timestamp: Date;
}

function SandboxTerminal({ sandboxId }: SandboxTerminalProps) {
  const [command, setCommand] = useState('');
  const [history, setHistory] = useState<CommandOutput[]>([]);
  const [historyIndex, setHistoryIndex] = useState(-1);
  const [copiedId, setCopiedId] = useState<string | null>(null);
  const outputRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  const executeMutation = useExecuteCommand(sandboxId);

  useEffect(() => {
    if (outputRef.current) {
      outputRef.current.scrollTop = outputRef.current.scrollHeight;
    }
  }, [history]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!command.trim() || executeMutation.isPending) return;

    const cmd = command.trim();
    setCommand('');
    setHistoryIndex(-1);

    try {
      const result = await executeMutation.mutateAsync({ command: cmd });

      setHistory((prev) => [
        ...prev,
        {
          id: Date.now().toString(),
          command: cmd,
          stdout: result.stdout,
          stderr: result.stderr,
          exitCode: result.exitCode,
          timestamp: new Date(),
        },
      ]);
    } catch (error) {
      setHistory((prev) => [
        ...prev,
        {
          id: Date.now().toString(),
          command: cmd,
          stdout: '',
          stderr: (error as Error).message,
          exitCode: -1,
          timestamp: new Date(),
        },
      ]);
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowUp') {
      e.preventDefault();
      const commands = history.map((h) => h.command);
      if (historyIndex < commands.length - 1) {
        const newIndex = historyIndex + 1;
        setHistoryIndex(newIndex);
        setCommand(commands[commands.length - 1 - newIndex]);
      }
    } else if (e.key === 'ArrowDown') {
      e.preventDefault();
      if (historyIndex > 0) {
        const newIndex = historyIndex - 1;
        setHistoryIndex(newIndex);
        const commands = history.map((h) => h.command);
        setCommand(commands[commands.length - 1 - newIndex]);
      } else if (historyIndex === 0) {
        setHistoryIndex(-1);
        setCommand('');
      }
    }
  };

  const copyOutput = async (output: CommandOutput) => {
    const text = output.stdout || output.stderr;
    await navigator.clipboard.writeText(text);
    setCopiedId(output.id);
    setTimeout(() => setCopiedId(null), 2000);
  };

  const clearHistory = () => {
    setHistory([]);
  };

  return (
    <div className="flex flex-col h-[600px] rounded-lg border bg-zinc-950 overflow-hidden">
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-2 bg-zinc-900 border-b border-zinc-800">
        <div className="flex items-center gap-2">
          <div className="flex gap-1.5">
            <div className="w-3 h-3 rounded-full bg-red-500" />
            <div className="w-3 h-3 rounded-full bg-yellow-500" />
            <div className="w-3 h-3 rounded-full bg-green-500" />
          </div>
          <span className="ml-2 text-sm text-zinc-400 font-mono">
            sandbox-{sandboxId.slice(0, 8)}
          </span>
        </div>
        <Button
          variant="ghost"
          size="sm"
          className="h-7 text-zinc-400 hover:text-zinc-100"
          onClick={clearHistory}
          disabled={history.length === 0}
        >
          <Trash2 className="h-3.5 w-3.5 mr-1" />
          Clear
        </Button>
      </div>

      {/* Output */}
      <div
        ref={outputRef}
        className="flex-1 overflow-auto p-4 font-mono text-sm text-zinc-100"
        onClick={() => inputRef.current?.focus()}
      >
        {history.length === 0 && (
          <div className="text-zinc-500">
            <p>Welcome to Isola Terminal</p>
            <p className="mt-1">Type a command and press Enter to execute.</p>
            <p className="mt-1 text-xs">
              Tip: Use arrow keys to navigate command history.
            </p>
          </div>
        )}

        {history.map((item) => (
          <div key={item.id} className="mb-4 group">
            {/* Command prompt */}
            <div className="flex items-center gap-2 text-zinc-400">
              <span className="text-emerald-400">$</span>
              <span className="text-zinc-100">{item.command}</span>
              <span className="text-zinc-600 text-xs ml-auto opacity-0 group-hover:opacity-100 transition-opacity">
                {item.timestamp.toLocaleTimeString()}
              </span>
            </div>

            {/* Output */}
            {(item.stdout || item.stderr) && (
              <div className="mt-1 relative group/output">
                {item.stdout && (
                  <pre className="whitespace-pre-wrap break-all text-zinc-300">
                    {item.stdout}
                  </pre>
                )}
                {item.stderr && (
                  <pre className="whitespace-pre-wrap break-all text-red-400">
                    {item.stderr}
                  </pre>
                )}

                {/* Copy button */}
                <button
                  onClick={() => copyOutput(item)}
                  className="absolute top-0 right-0 p-1.5 rounded bg-zinc-800 text-zinc-400 hover:text-zinc-100 opacity-0 group-hover/output:opacity-100 transition-opacity"
                >
                  {copiedId === item.id ? (
                    <Check className="h-3.5 w-3.5" />
                  ) : (
                    <Copy className="h-3.5 w-3.5" />
                  )}
                </button>
              </div>
            )}

            {/* Exit code */}
            {item.exitCode !== 0 && (
              <div className="mt-1 flex items-center gap-1 text-xs text-red-400">
                <AlertCircle className="h-3 w-3" />
                Exit code: {item.exitCode}
              </div>
            )}
          </div>
        ))}

        {/* Loading indicator */}
        {executeMutation.isPending && (
          <div className="flex items-center gap-2 text-zinc-400">
            <Loader2 className="h-4 w-4 animate-spin" />
            <span>Executing...</span>
          </div>
        )}
      </div>

      {/* Input */}
      <form onSubmit={handleSubmit} className="border-t border-zinc-800">
        <div className="flex items-center gap-2 px-4 py-3 bg-zinc-900">
          <span className="text-emerald-400 font-mono">$</span>
          <input
            ref={inputRef}
            type="text"
            value={command}
            onChange={(e) => setCommand(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder="Enter command..."
            className="flex-1 bg-transparent text-zinc-100 font-mono text-sm outline-none placeholder:text-zinc-600"
            disabled={executeMutation.isPending}
            autoFocus
          />
          <Button
            type="submit"
            size="sm"
            disabled={!command.trim() || executeMutation.isPending}
            className="h-8"
          >
            {executeMutation.isPending ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : (
              <Send className="h-4 w-4" />
            )}
          </Button>
        </div>
      </form>
    </div>
  );
}

export { SandboxTerminal };
