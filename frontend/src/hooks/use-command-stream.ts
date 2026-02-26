import { useEffect, useRef, useState } from 'react'
import { commandApi } from '@/api/commands'
import { useTerminalStore } from '@/stores/terminal-store'

function abortableDelay(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal.aborted) { reject(signal.reason); return }
    const timer = globalThis.setTimeout(resolve, ms)
    signal.addEventListener('abort', () => {
      clearTimeout(timer)
      reject(signal.reason)
    }, { once: true })
  })
}

export function useCommandStream(
  sandboxId: string,
  commandId: string,
  stream: 'stdout' | 'stderr',
  enabled: boolean,
) {
  const [status, setStatus] = useState<'idle' | 'streaming' | 'ended' | 'error'>('idle')
  const offsetRef = useRef(0)
  const abortRef = useRef<AbortController | null>(null)
  // Capture appendOutput once via ref to avoid re-triggering the effect
  const appendOutputRef = useRef(useTerminalStore.getState().appendOutput)

  useEffect(() => {
    if (!enabled || !commandId) return

    offsetRef.current = 0
    setStatus('streaming')
    const abortController = new AbortController()
    abortRef.current = abortController
    const { signal } = abortController

    let cancelled = false

    const run = async () => {
      let retries = 0
      const maxRetries = 5
      const baseDelay = 1000

      while (!signal.aborted) {
        try {
          const fetchFn = stream === 'stdout' ? commandApi.streamStdout : commandApi.streamStderr
          const res = await fetchFn(sandboxId, commandId, offsetRef.current, signal)
          if (!res.body) {
            if (!cancelled) setStatus('error')
            return
          }
          const reader = res.body.getReader()
          retries = 0

          while (true) {
            const { done, value } = await reader.read()
            if (done) {
              if (!cancelled) setStatus('ended')
              return
            }
            offsetRef.current += value.byteLength
            appendOutputRef.current(commandId, stream, value)
          }
        } catch {
          if (signal.aborted) return

          retries++
          if (retries > maxRetries) {
            if (!cancelled) setStatus('error')
            return
          }

          try {
            const delay = baseDelay * Math.pow(2, retries - 1) + Math.random() * 500
            await abortableDelay(delay, signal)
          } catch {
            return // aborted during delay
          }
        }
      }
    }

    run()

    return () => {
      cancelled = true
      abortController.abort()
    }
  }, [sandboxId, commandId, stream, enabled])

  return { status }
}
