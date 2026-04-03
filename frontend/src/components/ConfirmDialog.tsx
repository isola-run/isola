import { useEffect, useRef, useCallback } from "react";

interface ConfirmDialogProps {
  open: boolean;
  title: string;
  message: string;
  confirmLabel?: string;
  onConfirm: () => void;
  onCancel: () => void;
  destructive?: boolean;
}

export default function ConfirmDialog({
  open,
  title,
  message,
  confirmLabel = "Confirm",
  onConfirm,
  onCancel,
  destructive = false,
}: ConfirmDialogProps) {
  const dialogRef = useRef<HTMLDivElement>(null);
  const onCancelRef = useRef(onCancel);
  onCancelRef.current = onCancel;

  useEffect(() => {
    if (!open) return;
    const handleKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        onCancelRef.current();
        return;
      }
      if (e.key === "Tab") {
        // Focus trap: cycle between focusable elements in the dialog
        const dialog = dialogRef.current;
        if (!dialog) return;
        const focusable = dialog.querySelectorAll<HTMLElement>(
          'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])'
        );
        if (focusable.length === 0) return;
        const first = focusable[0];
        const last = focusable[focusable.length - 1];
        if (e.shiftKey && document.activeElement === first) {
          e.preventDefault();
          last.focus();
        } else if (!e.shiftKey && document.activeElement === last) {
          e.preventDefault();
          first.focus();
        }
      }
    };
    document.addEventListener("keydown", handleKey);
    return () => document.removeEventListener("keydown", handleKey);
  }, [open]);

  const focusFirst = useCallback((node: HTMLDivElement | null) => {
    if (node) {
      const btn = node.querySelector<HTMLElement>("button");
      btn?.focus();
    }
  }, []);

  if (!open) return null;

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center"
      role="dialog"
      aria-modal="true"
      aria-labelledby="confirm-title"
    >
      <div
        className="absolute inset-0 bg-black/60 backdrop-blur-sm"
        aria-hidden="true"
      />
      <div
        ref={(node) => {
          (dialogRef as React.MutableRefObject<HTMLDivElement | null>).current = node;
          focusFirst(node);
        }}
        className="relative bg-gray-900 border border-gray-700 rounded-lg p-6 max-w-sm w-full mx-4 shadow-xl"
      >
        <h2 id="confirm-title" className="text-lg font-semibold mb-2">
          {title}
        </h2>
        <p className="text-sm text-gray-400 mb-6">{message}</p>
        <div className="flex justify-end gap-3">
          <button onClick={onCancel} className="btn-secondary">
            Cancel
          </button>
          <button
            onClick={onConfirm}
            className={destructive ? "btn-danger" : "btn-primary"}
          >
            {confirmLabel}
          </button>
        </div>
      </div>
    </div>
  );
}
