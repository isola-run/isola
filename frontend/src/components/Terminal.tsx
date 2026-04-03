import { useState, useRef, useEffect, useCallback, useMemo } from "react";
import AnsiToHtml from "ansi-to-html";
import { api } from "../api/client";
import { getErrorMessage } from "../types";

interface TerminalProps {
  sandboxId: string;
}

interface CommandEntry {
  id: string;
  input: string;
  stdout: string;
  stderr: string;
  exitCode: number | null;
  running: boolean;
}

const MAX_OUTPUT_BYTES = 1024 * 1024; // 1MB per stream

function AnsiText({ text, className }: { text: string; className?: string }) {
  const converter = useMemo(
    () => new AnsiToHtml({ fg: "#d1d5db", bg: "transparent", escapeXML: true }),
    []
  );
  const html = useMemo(() => converter.toHtml(text), [converter, text]);
  return (
    <pre
      className={`whitespace-pre-wrap mt-0.5 ml-4 ${className ?? ""}`}
      dangerouslySetInnerHTML={{ __html: html }}
    />
  );
}

export default function Terminal({ sandboxId }: TerminalProps) {
  const [commands, setCommands] = useState<CommandEntry[]>([]);
  const [input, setInput] = useState("");
  const [cwd, setCwd] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [historyIndex, setHistoryIndex] = useState(-1);
  const outputRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const activeCleanups = useRef<Set<() => void>>(new Set());
  const historyRef = useRef<string[]>([]);

  const scrollToBottom = useCallback(() => {
    const el = outputRef.current;
    if (!el) return;
    const isNearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 50;
    if (isNearBottom) el.scrollTop = el.scrollHeight;
  }, []);

  useEffect(() => {
    return () => {
      activeCleanups.current.forEach((fn) => fn());
    };
  }, []);

  useEffect(scrollToBottom, [commands, scrollToBottom]);

  const handleKeyDown = (e: React.KeyboardEvent) => {
    const history = historyRef.current;
    if (e.key === "ArrowUp") {
      e.preventDefault();
      const nextIdx = historyIndex + 1;
      if (nextIdx < history.length) {
        setHistoryIndex(nextIdx);
        setInput(history[history.length - 1 - nextIdx]);
      }
    } else if (e.key === "ArrowDown") {
      e.preventDefault();
      const nextIdx = historyIndex - 1;
      if (nextIdx < 0) {
        setHistoryIndex(-1);
        setInput("");
      } else {
        setHistoryIndex(nextIdx);
        setInput(history[history.length - 1 - nextIdx]);
      }
    }
  };

  const handleClear = () => {
    setCommands([]);
  };

  const runCommand = async (e: React.FormEvent) => {
    e.preventDefault();
    const cmd = input.trim();
    if (!cmd || submitting) return;
    setInput("");
    setHistoryIndex(-1);
    setSubmitting(true);

    historyRef.current.push(cmd);
    const args = parseCommand(cmd);
    try {
      const resp = await api.createCommand(sandboxId, {
        args,
        ...(cwd ? { cwd } : {}),
      });

      const entry: CommandEntry = {
        id: resp.id,
        input: cmd,
        stdout: "",
        stderr: "",
        exitCode: null,
        running: true,
      };

      setCommands((prev) => [...prev, entry]);

      const update = (field: "stdout" | "stderr", text: string) => {
        setCommands((prev) =>
          prev.map((c) => {
            if (c.id !== resp.id) return c;
            const newVal = c[field] + text;
            return {
              ...c,
              [field]:
                newVal.length > MAX_OUTPUT_BYTES
                  ? newVal.slice(newVal.length - MAX_OUTPUT_BYTES)
                  : newVal,
            };
          })
        );
      };

      let stdoutDone = false;
      let stderrDone = false;

      const markDone = (exitCode: number | null) => {
        setCommands((prev) =>
          prev.map((c) =>
            c.id === resp.id
              ? { ...c, exitCode, running: false }
              : c
          )
        );
      };

      const checkDone = () => {
        if (stdoutDone && stderrDone) {
          api
            .getCommandStatus(sandboxId, resp.id, 20)
            .then((status) => markDone(status.exitCode))
            .catch(() => markDone(-1));
        }
      };

      const cleanupStdout = api.streamStdout(
        sandboxId,
        resp.id,
        (text) => update("stdout", text),
        () => {
          stdoutDone = true;
          activeCleanups.current.delete(cleanupStdout);
          checkDone();
        }
      );
      const cleanupStderr = api.streamStderr(
        sandboxId,
        resp.id,
        (text) => update("stderr", text),
        () => {
          stderrDone = true;
          activeCleanups.current.delete(cleanupStderr);
          checkDone();
        }
      );

      activeCleanups.current.add(cleanupStdout);
      activeCleanups.current.add(cleanupStderr);
    } catch (err: unknown) {
      setCommands((prev) => [
        ...prev,
        {
          id: crypto.randomUUID(),
          input: cmd,
          stdout: "",
          stderr: getErrorMessage(err, "Command failed"),
          exitCode: -1,
          running: false,
        },
      ]);
    } finally {
      setSubmitting(false);
      inputRef.current?.focus();
    }
  };

  return (
    <div className="flex flex-col h-full rounded-lg border border-gray-800 overflow-hidden bg-gray-950">
      <div className="flex items-center justify-between px-3 py-2 bg-gray-900/80 border-b border-gray-800">
        <div className="flex items-center gap-3">
          <span className="text-xs text-gray-400 font-mono">Terminal</span>
          {commands.length > 0 && (
            <button
              onClick={handleClear}
              className="text-xs text-gray-500 hover:text-gray-300 transition-colors"
            >
              Clear
            </button>
          )}
        </div>
        <div className="flex items-center gap-2">
          <label htmlFor="terminal-cwd" className="text-xs text-gray-500">cwd:</label>
          <input
            id="terminal-cwd"
            type="text"
            value={cwd}
            onChange={(e) => setCwd(e.target.value)}
            className="bg-gray-800 border border-gray-700 rounded px-2 py-0.5 text-xs font-mono text-gray-300 w-32 sm:w-40"
            placeholder="/workspace"
          />
        </div>
      </div>

      <div
        ref={outputRef}
        className="flex-1 overflow-y-auto p-3 font-mono text-sm min-h-[300px] max-h-[600px]"
        onClick={() => inputRef.current?.focus()}
      >
        {commands.length === 0 && (
          <div className="text-gray-600 text-xs">
            Type a command and press Enter. Use Up/Down arrows for history.
          </div>
        )}
        {commands.map((cmd) => (
          <div key={cmd.id} className="mb-3">
            <div className="flex items-center gap-2">
              <span className="text-indigo-400">$</span>
              <span className="text-gray-200">{cmd.input}</span>
              {cmd.running && (
                <span className="w-1.5 h-1.5 rounded-full bg-green-400 animate-pulse" aria-label="Running" />
              )}
              {cmd.exitCode !== null && cmd.exitCode !== 0 && (
                <span className="text-xs text-red-400">
                  exit {cmd.exitCode}
                </span>
              )}
            </div>
            {cmd.stdout && <AnsiText text={cmd.stdout} className="text-gray-300" />}
            {cmd.stderr && <AnsiText text={cmd.stderr} className="text-red-400" />}
          </div>
        ))}
      </div>

      <form
        onSubmit={runCommand}
        className="flex items-center border-t border-gray-800 bg-gray-900/50"
      >
        <span className="pl-3 text-indigo-400 font-mono text-sm">$</span>
        <input
          ref={inputRef}
          type="text"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={handleKeyDown}
          className="flex-1 bg-transparent border-none outline-none px-2 py-2.5 font-mono text-sm text-gray-200 placeholder-gray-600"
          placeholder="Enter command..."
          autoFocus
          disabled={submitting}
        />
        <button
          type="submit"
          disabled={submitting || !input.trim()}
          className="px-3 py-2.5 text-xs text-gray-400 hover:text-white disabled:opacity-30 transition-colors"
        >
          Run
        </button>
      </form>
    </div>
  );
}

function parseCommand(input: string): string[] {
  const args: string[] = [];
  let current = "";
  let inSingle = false;
  let inDouble = false;
  let escape = false;

  for (const ch of input) {
    if (escape) {
      current += ch;
      escape = false;
      continue;
    }
    if (ch === "\\") {
      escape = true;
      continue;
    }
    if (ch === "'" && !inDouble) {
      inSingle = !inSingle;
      continue;
    }
    if (ch === '"' && !inSingle) {
      inDouble = !inDouble;
      continue;
    }
    if ((ch === " " || ch === "\t") && !inSingle && !inDouble) {
      if (current) {
        args.push(current);
        current = "";
      }
      continue;
    }
    current += ch;
  }
  if (escape) current += "\\";
  if (current) args.push(current);
  return args;
}
