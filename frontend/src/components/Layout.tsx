import { Link, useLocation } from "react-router-dom";
import type { ReactNode } from "react";

export default function Layout({ children }: { children: ReactNode }) {
  const location = useLocation();
  const isSandboxes = location.pathname === "/sandboxes";

  return (
    <div className="min-h-screen flex flex-col">
      <header className="border-b border-gray-800 bg-gray-900/50 backdrop-blur-sm sticky top-0 z-50">
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
          <div className="flex items-center justify-between h-14">
            <Link to="/" className="flex items-center gap-2">
              <div className="w-7 h-7 rounded-lg bg-indigo-600 flex items-center justify-center font-bold text-sm">
                I
              </div>
              <span className="text-lg font-semibold">Isola</span>
            </Link>
            <nav className="flex items-center gap-1" aria-label="Main navigation">
              <Link
                to="/sandboxes"
                className={`px-3 py-1.5 rounded-md text-sm transition-colors ${
                  isSandboxes
                    ? "bg-gray-800 text-white"
                    : "text-gray-400 hover:text-white hover:bg-gray-800/50"
                }`}
                {...(isSandboxes ? { "aria-current": "page" as const } : {})}
              >
                Sandboxes
              </Link>
              <Link
                to="/sandboxes/new"
                className="ml-2 px-3 py-1.5 rounded-md text-sm bg-indigo-600 hover:bg-indigo-500 transition-colors"
              >
                + New Sandbox
              </Link>
            </nav>
          </div>
        </div>
      </header>
      <main className="flex-1 max-w-7xl w-full mx-auto px-4 sm:px-6 lg:px-8 py-6">
        {children}
      </main>
    </div>
  );
}
