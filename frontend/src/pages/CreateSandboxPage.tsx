import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { api } from "../api/client";
import { useToast } from "../components/Toast";
import { getErrorMessage } from "../types";
import type { CreateSandboxRequest } from "../types";

export default function CreateSandboxPage() {
  const navigate = useNavigate();
  const { toast } = useToast();
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

    const parsedTimeout = timeoutSeconds ? Number(timeoutSeconds) : undefined;
    if (parsedTimeout !== undefined && (!Number.isFinite(parsedTimeout) || parsedTimeout < 1)) {
      setError("Timeout must be a positive number");
      setSubmitting(false);
      return;
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
                  requests: {
                    ...(cpu ? { cpu } : {}),
                    ...(memory ? { memory } : {}),
                  },
                  limits: {
                    ...(cpu ? { cpu } : {}),
                    ...(memory ? { memory } : {}),
                  },
                },
              }
            : {}),
        },
      },
      ...(parsedTimeout !== undefined ? { timeoutSeconds: parsedTimeout } : {}),
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
      toast("Sandbox created", "success");
      navigate(`/sandboxes/${sandbox.id}`);
    } catch (err: unknown) {
      setError(getErrorMessage(err, "Failed to create sandbox"));
      setSubmitting(false);
    }
  };

  return (
    <div className="max-w-2xl mx-auto">
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
              className="input w-full"
              placeholder="alpine:3.21"
              required
            />
          </Field>
          <Field label="Command" hint="Override entrypoint (space-separated)">
            <input
              type="text"
              value={command}
              onChange={(e) => setCommand(e.target.value)}
              className="input w-full"
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
              className="input w-full"
              rows={3}
              placeholder={"FOO=bar\nDEBUG=true"}
            />
          </Field>
        </Section>

        <Section title="Resources">
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
            <Field label="CPU">
              <input
                type="text"
                value={cpu}
                onChange={(e) => setCpu(e.target.value)}
                className="input w-full"
                placeholder="250m"
              />
            </Field>
            <Field label="Memory">
              <input
                type="text"
                value={memory}
                onChange={(e) => setMemory(e.target.value)}
                className="input w-full"
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
              className="input w-full"
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
              className="w-4 h-4 rounded border-gray-600 bg-gray-800 text-indigo-600 focus:ring-indigo-500 focus:ring-offset-0"
            />
            <span className="text-sm">Allow internet egress</span>
          </label>
          <label className="flex items-center gap-2 cursor-pointer mt-2">
            <input
              type="checkbox"
              checked={allowDNS}
              onChange={(e) => setAllowDNS(e.target.checked)}
              className="w-4 h-4 rounded border-gray-600 bg-gray-800 text-indigo-600 focus:ring-indigo-500 focus:ring-offset-0"
            />
            <span className="text-sm">Allow cluster DNS</span>
          </label>
        </Section>

        <div className="flex gap-3 pt-2">
          <button
            type="submit"
            disabled={submitting || !image.trim()}
            className="btn-primary"
          >
            {submitting ? "Creating..." : "Create Sandbox"}
          </button>
          <button
            type="button"
            onClick={() => navigate("/sandboxes")}
            className="btn-secondary"
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
