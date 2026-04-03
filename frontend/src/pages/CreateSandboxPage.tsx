import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { api } from "../api/client";
import type { CreateSandboxRequest, ApiError } from "../types";

export default function CreateSandboxPage() {
  const navigate = useNavigate();
  const [image, setImage] = useState("alpine:3.21");
  const [command, setCommand] = useState("");
  const [envText, setEnvText] = useState("");
  const [timeoutSeconds, setTimeoutSeconds] = useState("");
  const [cpu, setCpu] = useState("");
  const [memory, setMemory] = useState("");
  const [allowInternet, setAllowInternet] = useState(false);
  const [allowDNS, setAllowDNS] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setSubmitting(true);
    setError(null);

    const env: Record<string, string> = {};
    for (const line of envText.split("\n")) {
      const trimmed = line.trim();
      if (!trimmed) continue;
      const eqIdx = trimmed.indexOf("=");
      if (eqIdx > 0) {
        env[trimmed.slice(0, eqIdx)] = trimmed.slice(eqIdx + 1);
      }
    }

    const req: CreateSandboxRequest = {
      podTemplate: {
        container: {
          image,
          ...(command.trim()
            ? { command: command.split(/\s+/).filter(Boolean) }
            : {}),
          ...(Object.keys(env).length > 0 ? { env } : {}),
          ...(cpu || memory
            ? {
                resources: {
                  ...(cpu || memory
                    ? {
                        requests: {
                          ...(cpu ? { cpu } : {}),
                          ...(memory ? { memory } : {}),
                        },
                      }
                    : {}),
                  ...(cpu || memory
                    ? {
                        limits: {
                          ...(cpu ? { cpu } : {}),
                          ...(memory ? { memory } : {}),
                        },
                      }
                    : {}),
                },
              }
            : {}),
        },
      },
      ...(timeoutSeconds ? { timeoutSeconds: parseInt(timeoutSeconds) } : {}),
      ...(allowInternet || allowDNS
        ? {
            network: {
              ...(allowInternet ? { allowInternetEgress: true } : {}),
              ...(allowDNS ? { allowClusterDNS: true } : {}),
            },
          }
        : {}),
    };

    try {
      const sandbox = await api.createSandbox(req);
      navigate(`/sandboxes/${sandbox.id}`);
    } catch (err: unknown) {
      const apiErr = err as ApiError;
      setError(apiErr.detail || apiErr.title || "Failed to create sandbox");
      setSubmitting(false);
    }
  };

  return (
    <div className="max-w-2xl">
      <h1 className="text-2xl font-bold mb-6">Create Sandbox</h1>

      {error && (
        <div className="mb-4 rounded-lg border border-red-800 bg-red-950/50 p-3 text-red-300 text-sm">
          {error}
        </div>
      )}

      <form onSubmit={handleSubmit} className="space-y-6">
        <Section title="Container">
          <Field label="Image" required>
            <input
              type="text"
              value={image}
              onChange={(e) => setImage(e.target.value)}
              className="input"
              placeholder="alpine:3.21"
              required
            />
          </Field>
          <Field label="Command" hint="Override entrypoint (space-separated)">
            <input
              type="text"
              value={command}
              onChange={(e) => setCommand(e.target.value)}
              className="input"
              placeholder="sleep infinity"
            />
          </Field>
          <Field
            label="Environment Variables"
            hint="One per line: KEY=value"
          >
            <textarea
              value={envText}
              onChange={(e) => setEnvText(e.target.value)}
              className="input"
              rows={3}
              placeholder={"FOO=bar\nDEBUG=true"}
            />
          </Field>
        </Section>

        <Section title="Resources">
          <div className="grid grid-cols-2 gap-4">
            <Field label="CPU">
              <input
                type="text"
                value={cpu}
                onChange={(e) => setCpu(e.target.value)}
                className="input"
                placeholder="250m"
              />
            </Field>
            <Field label="Memory">
              <input
                type="text"
                value={memory}
                onChange={(e) => setMemory(e.target.value)}
                className="input"
                placeholder="512Mi"
              />
            </Field>
          </div>
        </Section>

        <Section title="Lifecycle">
          <Field label="Timeout (seconds)" hint="Max sandbox lifetime">
            <input
              type="number"
              value={timeoutSeconds}
              onChange={(e) => setTimeoutSeconds(e.target.value)}
              className="input"
              placeholder="No timeout"
              min={1}
            />
          </Field>
        </Section>

        <Section title="Network">
          <label className="flex items-center gap-2 cursor-pointer">
            <input
              type="checkbox"
              checked={allowInternet}
              onChange={(e) => setAllowInternet(e.target.checked)}
              className="rounded border-gray-600 bg-gray-800 text-indigo-600 focus:ring-indigo-500"
            />
            <span className="text-sm">Allow internet egress</span>
          </label>
          <label className="flex items-center gap-2 cursor-pointer mt-2">
            <input
              type="checkbox"
              checked={allowDNS}
              onChange={(e) => setAllowDNS(e.target.checked)}
              className="rounded border-gray-600 bg-gray-800 text-indigo-600 focus:ring-indigo-500"
            />
            <span className="text-sm">Allow cluster DNS</span>
          </label>
        </Section>

        <div className="flex gap-3 pt-2">
          <button
            type="submit"
            disabled={submitting || !image.trim()}
            className="px-5 py-2 rounded-lg bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 disabled:cursor-not-allowed text-sm font-medium transition-colors"
          >
            {submitting ? "Creating..." : "Create Sandbox"}
          </button>
          <button
            type="button"
            onClick={() => navigate("/sandboxes")}
            className="px-5 py-2 rounded-lg border border-gray-700 hover:bg-gray-800 text-sm transition-colors"
          >
            Cancel
          </button>
        </div>
      </form>
    </div>
  );
}

function Section({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}) {
  return (
    <div className="rounded-lg border border-gray-800 p-4">
      <h2 className="text-sm font-medium text-gray-300 mb-3">{title}</h2>
      <div className="space-y-3">{children}</div>
    </div>
  );
}

function Field({
  label,
  hint,
  required,
  children,
}: {
  label: string;
  hint?: string;
  required?: boolean;
  children: React.ReactNode;
}) {
  return (
    <div>
      <label className="block text-xs text-gray-400 mb-1">
        {label}
        {required && <span className="text-red-400 ml-0.5">*</span>}
        {hint && <span className="text-gray-600 ml-1">({hint})</span>}
      </label>
      {children}
    </div>
  );
}
