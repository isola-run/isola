import { Badge } from '@/components/ui'
import type { SandboxState } from '@/types'

interface SandboxStatusBadgeProps {
  state: SandboxState
  size?: 'sm' | 'md'
}

const stateConfig: Record<
  SandboxState,
  { variant: 'success' | 'warning' | 'danger' | 'info' | 'neutral'; label: string; pulse?: boolean }
> = {
  pending: { variant: 'neutral', label: 'Pending' },
  starting: { variant: 'info', label: 'Starting', pulse: true },
  running: { variant: 'success', label: 'Running', pulse: true },
  terminating: { variant: 'warning', label: 'Terminating', pulse: true },
  stopped: { variant: 'neutral', label: 'Stopped' },
  error: { variant: 'danger', label: 'Error' },
  unknown: { variant: 'neutral', label: 'Unknown' },
}

export function SandboxStatusBadge({ state, size = 'md' }: SandboxStatusBadgeProps) {
  const config = stateConfig[state] || stateConfig.unknown

  return (
    <Badge variant={config.variant} size={size} dot pulse={config.pulse}>
      {config.label}
    </Badge>
  )
}
