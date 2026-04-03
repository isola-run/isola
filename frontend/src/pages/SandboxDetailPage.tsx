import { useState, useCallback } from "react";
import { useParams, useNavigate } from "react-router-dom";
import { api } from "../api/client";
import { usePolling } from "../hooks/usePolling";
import StatusBadge from "../components/StatusBadge";
import Terminal from "../components/Terminal";
import FileManager from "../components/FileManager";
import SnapshotPanel from "../components/SnapshotPanel";
import type { ApiError } from "../types";

type Tab = "terminal" | "files" | "snapshots";

export default function SandboxDetailPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [tab, setTab] = useState<Tab>("terminal");
  const [deleting, setDeleting] = useState(false);

  const fetcher = useCallback(() => api.getSandbox(id!), [id]);
  const { data: sandbox, error, loading } = usePolling(fetcher, 5000);

  const handleDelete = async () => {
    if (!confirm("Delete this sandbox? This cannot be undone.")) return;
    setDeleting(true);
    try {
      await api.deleteSandbox(id!);
      navigate("/sandboxes");
    } catch (err: unknown) {
      const apiErr = err as ApiError;
      alert(apiErr.detail || apiErr.title || "Failed to delete");
      setDeleting(false);
    }
  };

  if (loading && !sandbox) {
    return (
      <div className="text-center py-20 text-gray-500">
        <div className="inline-block w-6 h-6 border-2 border-gray-600 border-t-indigo-500 rounded-full animate-spin" />
        <p className="mt-3">Loading sandbox...</p>
      </div>
    );
  }

  if (error && !sandbox) {
    return (
      <div className="rounded-lg border border-red-800 bg-red-950/50 p-4 text-red-300 text-sm">
        {error}
      </div>
    );
  }

  if (!sandbox) return null;

  const isRunning = sandbox.status === "running";
  const tabs: { key: Tab; label: string }[] = [
    { key: "terminal", label: "Terminal" },
    { key: "files", label: "Files" },
    { key: "snapshots", label: "Snapshots" },
  ];

  return (
    <div>
      {/* Header */}
      <div className="flex items-start justify-between mb-6">
        <div>
          <div className="flex items-center gap-3">
            <h1 className="text-xl font-bold font-mono">{sandbox.id}</h1>
            <StatusBadge status={sandbox.status} />
          </div>
          <div className="mt-2 flex flex-wrap gap-x-6 gap-y-1 text-sm text-gray-400">
            <span>
              Image:{" "}
              <span className="text-gray-300">
                {sandbox.podTemplate.container.image}
              </span>
            </span>
            {sandbox.podTemplate.container.command && (
              <span>
                Cmd:{" "}
                <span className="font-mono text-gray-300">
                  {sandbox.podTemplate.container.command.join(" ")}
                </span>
              </span>
            )}
            <span>
              Created:{" "}
              <span className="text-gray-300">
                {new Date(sandbox.creationTimestamp).toLocaleString()}
              </span>
            </span>
            {sandbox.timeoutSeconds && (
              <span>
                Timeout:{" "}
                <span className="text-gray-300">
                  {sandbox.timeoutSeconds}s
                </span>
              </span>
            )}
          </div>

          {/* Network info */}
          {sandbox.network && (
            <div className="mt-1.5 flex gap-2">
              {sandbox.network.allowInternetEgress && (
                <span className="text-xs px-2 py-0.5 rounded bg-blue-900/30 text-blue-300 border border-blue-800">
                  Internet
                </span>
              )}
              {sandbox.network.allowClusterDNS && (
                <span className="text-xs px-2 py-0.5 rounded bg-blue-900/30 text-blue-300 border border-blue-800">
                  Cluster DNS
                </span>
              )}
            </div>
          )}

          {/* Resources */}
          {sandbox.podTemplate.container.resources && (
            <div className="mt-1.5 flex gap-3 text-xs text-gray-500">
              {sandbox.podTemplate.container.resources.requests?.cpu && (
                <span>
                  CPU: {sandbox.podTemplate.container.resources.requests.cpu}
                </span>
              )}
              {sandbox.podTemplate.container.resources.requests?.memory && (
                <span>
                  Mem:{" "}
                  {sandbox.podTemplate.container.resources.requests.memory}
                </span>
              )}
            </div>
          )}
        </div>
        <button
          onClick={handleDelete}
          disabled={deleting}
          className="px-3 py-1.5 rounded-md text-sm border border-red-800 text-red-400 hover:bg-red-950 disabled:opacity-50 transition-colors"
        >
          {deleting ? "Deleting..." : "Delete"}
        </button>
      </div>

      {/* Tabs */}
      <div className="border-b border-gray-800 mb-4">
        <div className="flex gap-0">
          {tabs.map((t) => (
            <button
              key={t.key}
              onClick={() => setTab(t.key)}
              className={`px-4 py-2 text-sm font-medium border-b-2 transition-colors ${
                tab === t.key
                  ? "border-indigo-500 text-white"
                  : "border-transparent text-gray-400 hover:text-gray-200"
              }`}
            >
              {t.label}
            </button>
          ))}
        </div>
      </div>

      {/* Tab content */}
      {tab === "terminal" && (
        isRunning ? (
          <Terminal sandboxId={sandbox.id} />
        ) : (
          <div className="text-center py-10 text-gray-500 text-sm">
            Terminal is only available for running sandboxes.
          </div>
        )
      )}
      {tab === "files" && (
        isRunning ? (
          <FileManager sandboxId={sandbox.id} />
        ) : (
          <div className="text-center py-10 text-gray-500 text-sm">
            File manager is only available for running sandboxes.
          </div>
        )
      )}
      {tab === "snapshots" && <SnapshotPanel sandboxId={sandbox.id} />}
    </div>
  );
}
