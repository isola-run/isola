import { useState, useCallback } from "react";
import { api } from "../api/client";
import { usePolling } from "../hooks/usePolling";
import StatusBadge from "./StatusBadge";
import { useToast } from "./Toast";
import { getErrorMessage } from "../types";
import type { RootfsSnapshotResponse } from "../types";

interface SnapshotPanelProps {
  sandboxId: string;
}

function isActive(s: RootfsSnapshotResponse): boolean {
  return s.status === "pending" || s.status === "inProgress";
}

export default function SnapshotPanel({ sandboxId }: SnapshotPanelProps) {
  const [snapshotName, setSnapshotName] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [snapshots, setSnapshots] = useState<RootfsSnapshotResponse[]>([]);
  const { toast } = useToast();

  const pollActive = useCallback(async () => {
    // Read active snapshots from state via the updater to avoid stale closure
    let activeIds: string[] = [];
    setSnapshots((prev) => {
      activeIds = prev.filter(isActive).map((s) => s.id);
      return prev;
    });
    if (activeIds.length === 0) return [];
    const updated = await Promise.all(
      activeIds.map((id) =>
        api.getSnapshot(id).catch(() => null)
      )
    );
    const results = updated.filter(
      (r): r is RootfsSnapshotResponse => r !== null
    );
    if (results.length > 0) {
      setSnapshots((prev) =>
        prev.map((s) => results.find((u) => u.id === s.id) ?? s)
      );
    }
    return results;
  }, []);

  const hasActive = snapshots.some(isActive);
  usePolling(pollActive, 3000, hasActive);

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!snapshotName.trim()) return;
    setSubmitting(true);
    setError(null);
    try {
      const snapshot = await api.createSnapshot({
        sandboxId,
        snapshotName: snapshotName.trim(),
      });
      setSnapshots((prev) => [snapshot, ...prev]);
      setSnapshotName("");
      toast(`Snapshot "${snapshot.snapshotName}" created`, "success");
    } catch (err: unknown) {
      setError(getErrorMessage(err, "Failed to create snapshot"));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="space-y-4">
      <div className="rounded-lg border border-gray-800 p-4">
        <h3 className="text-sm font-medium text-gray-300 mb-3">
          Create Rootfs Snapshot
        </h3>
        <form onSubmit={handleCreate} className="flex gap-2">
          <input
            type="text"
            value={snapshotName}
            onChange={(e) => setSnapshotName(e.target.value)}
            className="input flex-1"
            placeholder="snapshot-name (DNS label format)"
            pattern="^[a-z0-9]([-a-z0-9]*[a-z0-9])?$"
            aria-label="Snapshot name"
          />
          <button
            type="submit"
            disabled={submitting || !snapshotName.trim()}
            className="btn-secondary"
          >
            {submitting ? "Creating..." : "Snapshot"}
          </button>
        </form>
        <p className="mt-2 text-xs text-gray-500">
          Captures the overlay filesystem layer. Use this name as a
          rootfsSnapshotSource when creating new sandboxes.
        </p>
        {error && <p className="mt-2 text-sm text-red-400">{error}</p>}
      </div>

      {snapshots.length > 0 && (
        <div className="rounded-lg border border-gray-800 overflow-x-auto">
          <table className="w-full">
            <thead>
              <tr className="border-b border-gray-800 bg-gray-900/50">
                <th className="text-left px-4 py-2 text-xs font-medium text-gray-400 uppercase">
                  Name
                </th>
                <th className="text-left px-4 py-2 text-xs font-medium text-gray-400 uppercase">
                  Status
                </th>
                <th className="text-left px-4 py-2 text-xs font-medium text-gray-400 uppercase">
                  Created
                </th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-800">
              {snapshots.map((snap) => (
                <tr key={snap.id}>
                  <td className="px-4 py-2 font-mono text-sm text-gray-300">
                    {snap.snapshotName}
                  </td>
                  <td className="px-4 py-2">
                    <StatusBadge status={snap.status} />
                  </td>
                  <td className="px-4 py-2 text-sm text-gray-400">
                    {new Date(snap.creationTimestamp).toLocaleString()}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
