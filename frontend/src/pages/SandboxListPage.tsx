import { useCallback } from "react";
import { Link } from "react-router-dom";
import { api } from "../api/client";
import { usePolling } from "../hooks/usePolling";
import StatusBadge from "../components/StatusBadge";

export default function SandboxListPage() {
  const fetcher = useCallback(() => api.listSandboxes(), []);
  const { data, error, loading } = usePolling(fetcher, 3000);

  const sandboxes = data?.sandboxes ?? [];

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-2xl font-bold">Sandboxes</h1>
          <p className="text-sm text-gray-400 mt-1">
            Manage your isolated execution environments
          </p>
        </div>
        <Link
          to="/sandboxes/new"
          className="px-4 py-2 rounded-lg bg-indigo-600 hover:bg-indigo-500 text-sm font-medium transition-colors"
        >
          Create Sandbox
        </Link>
      </div>

      {loading && !data && (
        <div className="text-center py-20 text-gray-500">
          <div className="inline-block w-6 h-6 border-2 border-gray-600 border-t-indigo-500 rounded-full animate-spin" />
          <p className="mt-3">Loading sandboxes...</p>
        </div>
      )}

      {error && (
        <div className="rounded-lg border border-red-800 bg-red-950/50 p-4 text-red-300 text-sm">
          {error}
        </div>
      )}

      {!loading && sandboxes.length === 0 && !error && (
        <div className="text-center py-20">
          <div className="text-4xl mb-3 text-gray-600">&#9634;</div>
          <h2 className="text-lg font-medium text-gray-300">
            No sandboxes yet
          </h2>
          <p className="text-sm text-gray-500 mt-1">
            Create one to get started
          </p>
          <Link
            to="/sandboxes/new"
            className="inline-block mt-4 px-4 py-2 rounded-lg bg-indigo-600 hover:bg-indigo-500 text-sm font-medium transition-colors"
          >
            Create Sandbox
          </Link>
        </div>
      )}

      {sandboxes.length > 0 && (
        <div className="rounded-lg border border-gray-800 overflow-hidden">
          <table className="w-full">
            <thead>
              <tr className="border-b border-gray-800 bg-gray-900/50">
                <th className="text-left px-4 py-3 text-xs font-medium text-gray-400 uppercase tracking-wider">
                  ID
                </th>
                <th className="text-left px-4 py-3 text-xs font-medium text-gray-400 uppercase tracking-wider">
                  Status
                </th>
                <th className="text-left px-4 py-3 text-xs font-medium text-gray-400 uppercase tracking-wider">
                  Created
                </th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-800">
              {sandboxes.map((sb) => (
                <tr
                  key={sb.id}
                  className="hover:bg-gray-900/50 transition-colors"
                >
                  <td className="px-4 py-3">
                    <Link
                      to={`/sandboxes/${sb.id}`}
                      className="text-indigo-400 hover:text-indigo-300 font-mono text-sm"
                    >
                      {sb.id}
                    </Link>
                  </td>
                  <td className="px-4 py-3">
                    <StatusBadge status={sb.status} />
                  </td>
                  <td className="px-4 py-3 text-sm text-gray-400">
                    {new Date(sb.creationTimestamp).toLocaleString()}
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
