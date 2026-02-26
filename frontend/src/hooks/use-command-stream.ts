import { useEffect, useRef, useCallback, useState } from 'react'
import { commandApi } from '@/api/commands'
import { useTerminalStore } from '@/stores/terminal-store'

export function useCommandStream(
  sandboxId: string,
  commandId: string,
  stream: 'stdout' | 'stderr',
  enabled: boolean,
) {
  const appendOutput = useTerminalStore((s) => s.appendOutput)
  const [status, setStatus] = useState<'idle' | 'streaming' | 'ended' | 'error'>('idle')
  const offsetRef = useRef(0)
  const abortRef = useRef<AbortController | null>(null)

  const startStreaming = useCallback(async () => {
    if (!enabled || !commandId) return

    setStatus('streaming')
    const abortController = new AbortController()
    abortRef.current = abortController
    const { signal } = abortController

    let retries = 0
    const maxRetries = 5
    const baseDelay = 1000

    while (!signal.aborted) {
      try {
        const fetchFn = stream === 'stdout' ? commandApi.streamStdout : commandApi.streamStderr
        const res = await fetchFn(sandboxId, commandId, offsetRef.current, signal)
        const reader = res.body!.getReader()
        retries = 0

        while (true) {
          const { done, value } = await reader.read()
          if (done) {
            setStatus('ended')
            return
          }
          offsetRef.current += value.byteLength
          appendOutput(commandId, stream, value)
        }
      } catch (err) {
        if (signal.aborted) return

        retries++
        if (retries > maxRetries) {
          setStatus('error')
          return
        }

        const delay = baseDelay * Math.pow(2, retries - 1) + Math.random() * 500
        await new Promise((resolve) => setTimeout(resolve, delay))
      }
    }
  }, [sandboxId, commandId, stream, enabled, appendOutput])

  useEffect(() => {
    offsetRef.current = 0
    startStreaming()
    return () => {
      abortRef.current?.abort()
    }
  }, [startStreaming])

  return { status }
}
