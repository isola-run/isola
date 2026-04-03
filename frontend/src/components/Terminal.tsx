import { useState, useRef, useEffect, useCallback } from "react";
import { api } from "../api/client";
import type { ApiError } from "../types";

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

export default function Terminal({ sandboxId }: TerminalProps) {
  const [commands, setCommands] = useState<CommandEntry[]>([]);
  const [input, setInput] = useState("");
  const [cwd, setCwd] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const outputRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const cleanupRefs = useRef<Array<() => void>>([]);

  const scrollToBottom = useCallback(() => {
    if (outputRef.current) {
      outputRef.current.scrollTop = outputRef.current.scrollHeight;
    }
  }, []);

  useEffect(() => {
    return () => {
      cleanupRefs.current.forEach((fn) => fn());
    };
  }, []);

  useEffect(scrollToBottom, [commands, scrollToBottom]);

  const runCommand = async (e: React.FormEvent) => {
    e.preventDefault();
    const cmd = input.trim();
    if (!cmd || submitting) return;
    setInput("");
    setSubmitting(true);

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
          prev.map((c) =>
            c.id === resp.id ? { ...c, [field]: c[field] + text } : c
          )
        );
      };

      let stdoutDone = false;
      let stderrDone = false;

      const checkDone = () => {
        if (stdoutDone && stderrDone) {
          api.getCommandStatus(sandboxId, resp.id, 20).then((status) => {
            setCommands((prev) =>
              prev.map((c) =>
                c.id === resp.id
                  ? { ...c, exitCode: status.exitCode, running: false }
                  : c
              )
            );
          });
        }
      };

      const cleanupStdout = api.streamStdout(
        sandboxId,
        resp.id,
        (text) => update("stdout", text),
        () => {
          stdoutDone = true;
          checkDone();
        }
      );
      const cleanupStderr = api.streamStderr(
        sandboxId,
        resp.id,
        (text) => update("stderr", text),
        () => {
          stderrDone = true;
          checkDone();
        }
      );

      cleanupRefs.current.push(cleanupStdout, cleanupStderr);
    } catch (err: unknown) {
      const apiErr = err as ApiError;
      setCommands((prev) => [
        ...prev,
        {
          id: crypto.randomUUID(),
          input: cmd,
          stdout: "",
          stderr: apiErr.detail || apiErr.title || "Command failed",
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
        <span className="text-xs text-gray-400 font-mono">Terminal</span>
        <div className="flex items-center gap-2">
          <label className="text-xs text-gray-500">cwd:</label>
          <input
            type="text"
            value={cwd}
            onChange={(e) => setCwd(e.target.value)}
            className="bg-gray-800 border border-gray-700 rounded px-2 py-0.5 text-xs font-mono text-gray-300 w-40"
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
            Type a command and press Enter to execute it in the sandbox.
          </div>
        )}
        {commands.map((cmd) => (
          <div key={cmd.id} className="mb-3">
            <div className="flex items-center gap-2">
              <span className="text-indigo-400">$</span>
              <span className="text-gray-200">{cmd.input}</span>
              {cmd.running && (
                <span className="w-1.5 h-1.5 rounded-full bg-green-400 animate-pulse" />
              )}
              {cmd.exitCode !== null && cmd.exitCode !== 0 && (
                <span className="text-xs text-red-400">
                  exit {cmd.exitCode}
                </span>
              )}
            </div>
            {cmd.stdout && (
              <pre className="text-gray-300 whitespace-pre-wrap mt-0.5 ml-4">
                {cmd.stdout}
              </pre>
            )}
            {cmd.stderr && (
              <pre className="text-red-400 whitespace-pre-wrap mt-0.5 ml-4">
                {cmd.stderr}
              </pre>
            )}
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
    if (ch === " " && !inSingle && !inDouble) {
      if (current) {
        args.push(current);
        current = "";
      }
      continue;
    }
    current += ch;
  }
  if (current) args.push(current);
  return args;
}
