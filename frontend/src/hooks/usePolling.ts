import { useEffect, useRef, useState, useCallback } from "react";

export function usePolling<T>(
  fetcher: () => Promise<T>,
  intervalMs: number,
  enabled = true
) {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const mountedRef = useRef(true);

  const refresh = useCallback(async () => {
    try {
      const result = await fetcher();
      if (mountedRef.current) {
        setData(result);
        setError(null);
      }
    } catch (err: unknown) {
      if (mountedRef.current) {
        const msg =
          err && typeof err === "object" && "detail" in err
            ? String((err as { detail: string }).detail)
            : "Request failed";
        setError(msg);
      }
    } finally {
      if (mountedRef.current) setLoading(false);
    }
  }, [fetcher]);

  useEffect(() => {
    mountedRef.current = true;
    if (!enabled) return;
    refresh();
    const id = setInterval(refresh, intervalMs);
    return () => {
      mountedRef.current = false;
      clearInterval(id);
    };
  }, [refresh, intervalMs, enabled]);

  return { data, error, loading, refresh };
}
