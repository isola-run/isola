import { useState, useCallback } from "react";
import { useParams, useNavigate, Link } from "react-router-dom";
import { api } from "../api/client";
import { usePolling } from "../hooks/usePolling";
import StatusBadge from "../components/StatusBadge";
import Terminal from "../components/Terminal";
import FileManager from "../components/FileManager";
import SnapshotPanel from "../components/SnapshotPanel";
import ConfirmDialog from "../components/ConfirmDialog";
import Spinner from "../components/Spinner";
import { useToast } from "../components/Toast";
import { getErrorMessage } from "../types";

type Tab = "terminal" | "files" | "snapshots";

const TABS: { key: Tab; label: string }[] = [
  { key: "terminal", label: "Terminal" },
  { key: "files", label: "Files" },
  { key: "snapshots", label: "Snapshots" },
];

export default function SandboxDetailPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [tab, setTab] = useState<Tab>("terminal");
  const [deleting, setDeleting] = useState(false);
  const [showDeleteDialog, setShowDeleteDialog] = useState(false);
  const { toast } = useToast();

  if (!id) throw new Error("Missing sandbox id");

  const fetcher = useCallback(() => api.getSandbox(id), [id]);
  const { data: sandbox, error, loading } = usePolling(fetcher, 5000);

  const handleDelete = async () => {
    setShowDeleteDialog(false);
    setDeleting(true);
    try {
      await api.deleteSandbox(id);
      toast("Sandbox deleted", "success");
      navigate("/sandboxes");
    } catch (err: unknown) {
      toast(getErrorMessage(err, "Failed to delete"), "error");
      setDeleting(false);
    }
  };

  if (loading && !sandbox) {
    return <Spinner label="Loading sandbox..." />;
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

  return (
    <div>
      {/* Breadcrumb */}
      <nav className="mb-4 text-sm" aria-label="Breadcrumb">
        <ol className="flex items-center gap-1.5 text-gray-400">
          <li>
            <Link to="/sandboxes" className="hover:text-gray-200 transition-colors">
              Sandboxes
            </Link>
          </li>
          <li aria-hidden="true">/</li>
          <li className="text-gray-200 font-mono truncate max-w-[200px] sm:max-w-none">
            {sandbox.id}
          </li>
        </ol>
      </nav>

      {/* Stale data warning */}
      {error && sandbox && (
        <div className="mb-4 rounded-lg border border-yellow-800 bg-yellow-950/50 p-3 text-yellow-300 text-sm">
          Failed to refresh: {error}
        </div>
      )}

      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-start sm:justify-between gap-4 mb-6">
        <div className="min-w-0">
          <div className="flex items-center gap-3">
            <h1 className="text-xl font-bold font-mono truncate">{sandbox.id}</h1>
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
          onClick={() => setShowDeleteDialog(true)}
          disabled={deleting}
          className="btn-danger self-start"
        >
          {deleting ? "Deleting..." : "Delete"}
        </button>
      </div>

      {/* Tabs */}
      <div
        className="border-b border-gray-800 mb-4"
        role="tablist"
        aria-label="Sandbox sections"
        onKeyDown={(e) => {
          const keys = TABS.map((t) => t.key);
          const idx = keys.indexOf(tab);
          if (e.key === "ArrowRight") {
            e.preventDefault();
            setTab(keys[(idx + 1) % keys.length]);
          } else if (e.key === "ArrowLeft") {
            e.preventDefault();
            setTab(keys[(idx - 1 + keys.length) % keys.length]);
          }
        }}
      >
        <div className="flex gap-0">
          {TABS.map((t) => (
            <button
              key={t.key}
              id={`tab-${t.key}`}
              role="tab"
              aria-selected={tab === t.key}
              aria-controls={`tabpanel-${t.key}`}
              tabIndex={tab === t.key ? 0 : -1}
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
      <div role="tabpanel" id={`tabpanel-${tab}`} aria-labelledby={`tab-${tab}`}>
        {tab === "terminal" && (
          isRunning ? (
            <Terminal sandboxId={sandbox.id} />
          ) : (
            <div className="text-center py-10 text-gray-500 text-sm">
              Terminal is only available for running sandboxes.
              <br />
              <span className="text-xs text-gray-600">
                Current status: {sandbox.status}
              </span>
            </div>
          )
        )}
        {tab === "files" && (
          isRunning ? (
            <FileManager sandboxId={sandbox.id} />
          ) : (
            <div className="text-center py-10 text-gray-500 text-sm">
              File manager is only available for running sandboxes.
              <br />
              <span className="text-xs text-gray-600">
                Current status: {sandbox.status}
              </span>
            </div>
          )
        )}
        {tab === "snapshots" && <SnapshotPanel sandboxId={sandbox.id} />}
      </div>

      <ConfirmDialog
        open={showDeleteDialog}
        title="Delete Sandbox"
        message={`Are you sure you want to delete sandbox ${sandbox.id}? This action cannot be undone.`}
        confirmLabel="Delete"
        destructive
        onConfirm={handleDelete}
        onCancel={() => setShowDeleteDialog(false)}
      />
    </div>
  );
}
