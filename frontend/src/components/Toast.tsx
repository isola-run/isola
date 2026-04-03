import { useState, useEffect, useCallback, createContext, useContext } from "react";
import type { ReactNode } from "react";

interface Toast {
  id: string;
  message: string;
  type: "success" | "error" | "info";
}

interface ToastContextValue {
  toast: (message: string, type?: Toast["type"]) => void;
}

const ToastContext = createContext<ToastContextValue>({
  toast: () => {},
});

export function useToast() {
  return useContext(ToastContext);
}

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([]);

  const toast = useCallback((message: string, type: Toast["type"] = "info") => {
    const id = crypto.randomUUID();
    setToasts((prev) => {
      const next = [...prev, { id, message, type }];
      // Cap at 10 toasts to prevent accumulation under rapid error bursts
      return next.length > 10 ? next.slice(-10) : next;
    });
  }, []);

  const dismiss = useCallback((id: string) => {
    setToasts((prev) => prev.filter((t) => t.id !== id));
  }, []);

  return (
    <ToastContext.Provider value={{ toast }}>
      {children}
      <div className="fixed bottom-4 right-4 z-50 flex flex-col gap-2 max-w-sm">
        {toasts.map((t) => (
          <ToastItem key={t.id} toast={t} onDismiss={dismiss} />
        ))}
      </div>
    </ToastContext.Provider>
  );
}

function ToastItem({
  toast,
  onDismiss,
}: {
  toast: Toast;
  onDismiss: (id: string) => void;
}) {
  useEffect(() => {
    const timer = setTimeout(() => onDismiss(toast.id), 4000);
    return () => clearTimeout(timer);
  }, [toast.id, onDismiss]);

  const colors = {
    success: "border-green-700 bg-green-950/90 text-green-300",
    error: "border-red-700 bg-red-950/90 text-red-300",
    info: "border-gray-700 bg-gray-900/90 text-gray-300",
  };

  return (
    <div
      className={`rounded-lg border px-4 py-3 text-sm backdrop-blur-sm shadow-lg animate-slide-in ${colors[toast.type]}`}
      role={toast.type === "error" ? "alert" : "status"}
    >
      <div className="flex items-center justify-between gap-3">
        <span>{toast.message}</span>
        <button
          onClick={() => onDismiss(toast.id)}
          className="text-gray-500 hover:text-gray-300 text-xs"
          aria-label="Dismiss"
        >
          &#x2715;
        </button>
      </div>
    </div>
  );
}
