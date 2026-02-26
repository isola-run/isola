import { create } from 'zustand'

interface CommandOutput {
  stdout: Uint8Array[]
  stderr: Uint8Array[]
  stdoutText: string
  stderrText: string
  stdoutDecoder: TextDecoder
  stderrDecoder: TextDecoder
}

interface TerminalState {
  outputs: Map<string, CommandOutput>
  activeStream: Map<string, 'stdout' | 'stderr'>
  appendOutput: (cmdId: string, stream: 'stdout' | 'stderr', chunk: Uint8Array) => void
  setActiveStream: (cmdId: string, stream: 'stdout' | 'stderr') => void
  clearOutput: (cmdId: string) => void
  getOutput: (cmdId: string) => CommandOutput | undefined
}

function createCommandOutput(): CommandOutput {
  return {
    stdout: [],
    stderr: [],
    stdoutText: '',
    stderrText: '',
    stdoutDecoder: new TextDecoder('utf-8', { fatal: false }),
    stderrDecoder: new TextDecoder('utf-8', { fatal: false }),
  }
}

export const useTerminalStore = create<TerminalState>((set, get) => ({
  outputs: new Map(),
  activeStream: new Map(),

  appendOutput: (cmdId, stream, chunk) => {
    set((state) => {
      const outputs = new Map(state.outputs)
      const existing = outputs.get(cmdId) ?? createCommandOutput()

      const decoder = stream === 'stdout' ? existing.stdoutDecoder : existing.stderrDecoder
      const newChunks = [...existing[stream], chunk]
      const newText = existing[`${stream}Text`] + decoder.decode(chunk, { stream: true })

      outputs.set(cmdId, {
        ...existing,
        [stream]: newChunks,
        [`${stream}Text`]: newText,
      })

      return { outputs }
    })
  },

  setActiveStream: (cmdId, stream) => {
    set((state) => {
      const activeStream = new Map(state.activeStream)
      activeStream.set(cmdId, stream)
      return { activeStream }
    })
  },

  clearOutput: (cmdId) => {
    set((state) => {
      const outputs = new Map(state.outputs)
      outputs.delete(cmdId)
      return { outputs }
    })
  },

  getOutput: (cmdId) => {
    return get().outputs.get(cmdId)
  },
}))
