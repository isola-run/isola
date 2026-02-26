import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogBody, DialogFooter } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input, Label } from '@/components/ui/input'
import { useCreateSandbox } from '@/hooks/use-sandboxes'
import { toast } from 'sonner'
import { Loader2, ChevronDown, ChevronRight } from 'lucide-react'
import { cn } from '@/lib/utils'

interface CreateSandboxDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function CreateSandboxDialog({ open, onOpenChange }: CreateSandboxDialogProps) {
  const navigate = useNavigate()
  const createSandbox = useCreateSandbox()

  const [image, setImage] = useState('ubuntu:latest')
  const [cpu, setCpu] = useState('')
  const [memory, setMemory] = useState('')
  const [timeout, setTimeout] = useState('')
  const [allowInternet, setAllowInternet] = useState(false)
  const [showAdvanced, setShowAdvanced] = useState(false)
  const [envVars, setEnvVars] = useState<Array<{ key: string; value: string }>>([])

  // Reset form state when dialog opens
  useEffect(() => {
    if (open) {
      setImage('ubuntu:latest')
      setCpu('')
      setMemory('')
      setTimeout('')
      setAllowInternet(false)
      setShowAdvanced(false)
      setEnvVars([])
    }
  }, [open])

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()

    const env: Record<string, string> = {}
    for (const { key, value } of envVars) {
      if (key.trim()) env[key.trim()] = value
    }

    try {
      const sandbox = await createSandbox.mutateAsync({
        podTemplate: {
          container: {
            image,
            ...(Object.keys(env).length > 0 ? { env } : {}),
            ...((cpu || memory) ? {
              resources: {
                limits: {
                  ...(cpu ? { cpu } : {}),
                  ...(memory ? { memory } : {}),
                },
              },
            } : {}),
          },
        },
        ...(timeout && !isNaN(Number(timeout)) ? { activeDeadlineSeconds: Number(timeout) } : {}),
        ...(allowInternet ? { network: { allowInternetEgress: true } } : {}),
      })

      toast.success('Sandbox created')
      onOpenChange(false)
      navigate(`/sandboxes/${sandbox.id}`)
    } catch (err) {
      toast.error(`Failed to create sandbox: ${err instanceof Error ? err.message : 'Unknown error'}`)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>Create Sandbox</DialogTitle>
          <DialogDescription>
            Launch a new isolated container environment
          </DialogDescription>
        </DialogHeader>

        <form onSubmit={handleSubmit}>
          <DialogBody className="space-y-4">
            {/* Image */}
            <div className="space-y-1.5">
              <Label htmlFor="image">Container Image</Label>
              <Input
                id="image"
                value={image}
                onChange={(e) => setImage(e.target.value)}
                placeholder="ubuntu:latest"
                required
                autoFocus
              />
            </div>

            {/* Network */}
            <div className="flex items-center justify-between py-2 px-3 bg-bg-input rounded-lg border border-border-default">
              <div>
                <div className="text-sm font-medium text-text-primary">Internet Access</div>
                <div className="text-xs text-text-tertiary">Allow outbound network traffic</div>
              </div>
              <button
                type="button"
                role="switch"
                aria-checked={allowInternet}
                onClick={() => setAllowInternet(!allowInternet)}
                className={cn(
                  'relative w-9 h-5 rounded-full transition-colors duration-200',
                  allowInternet ? 'bg-accent' : 'bg-bg-active',
                )}
              >
                <span
                  className={cn(
                    'absolute top-0.5 left-0.5 w-4 h-4 rounded-full bg-white transition-transform duration-200',
                    allowInternet && 'translate-x-4',
                  )}
                />
              </button>
            </div>

            {/* Advanced toggle */}
            <button
              type="button"
              onClick={() => setShowAdvanced(!showAdvanced)}
              className="flex items-center gap-1.5 text-sm text-text-secondary hover:text-text-primary transition-colors"
            >
              {showAdvanced ? <ChevronDown className="w-3.5 h-3.5" /> : <ChevronRight className="w-3.5 h-3.5" />}
              Advanced options
            </button>

            {showAdvanced && (
              <div className="space-y-3 pl-3 border-l-2 border-border-subtle animate-slide-up">
                {/* Resources */}
                <div className="grid grid-cols-2 gap-3">
                  <div className="space-y-1.5">
                    <Label htmlFor="cpu">CPU Limit</Label>
                    <Input
                      id="cpu"
                      value={cpu}
                      onChange={(e) => setCpu(e.target.value)}
                      placeholder="250m"
                    />
                  </div>
                  <div className="space-y-1.5">
                    <Label htmlFor="memory">Memory Limit</Label>
                    <Input
                      id="memory"
                      value={memory}
                      onChange={(e) => setMemory(e.target.value)}
                      placeholder="512Mi"
                    />
                  </div>
                </div>

                {/* Timeout */}
                <div className="space-y-1.5">
                  <Label htmlFor="timeout">Timeout (seconds)</Label>
                  <Input
                    id="timeout"
                    type="number"
                    value={timeout}
                    onChange={(e) => setTimeout(e.target.value)}
                    placeholder="3600"
                  />
                </div>

                {/* Env vars */}
                <div className="space-y-1.5">
                  <div className="flex items-center justify-between">
                    <Label>Environment Variables</Label>
                    <button
                      type="button"
                      onClick={() => setEnvVars([...envVars, { key: '', value: '' }])}
                      className="text-xs text-accent hover:text-accent-hover transition-colors"
                    >
                      + Add variable
                    </button>
                  </div>
                  {envVars.map((env, i) => (
                    <div key={i} className="flex items-center gap-2">
                      <Input
                        value={env.key}
                        onChange={(e) => {
                          const newEnvs = [...envVars]
                          newEnvs[i] = { ...newEnvs[i], key: e.target.value }
                          setEnvVars(newEnvs)
                        }}
                        placeholder="KEY"
                        className="font-mono text-xs"
                      />
                      <Input
                        value={env.value}
                        onChange={(e) => {
                          const newEnvs = [...envVars]
                          newEnvs[i] = { ...newEnvs[i], value: e.target.value }
                          setEnvVars(newEnvs)
                        }}
                        placeholder="value"
                        className="font-mono text-xs"
                      />
                      <button
                        type="button"
                        onClick={() => setEnvVars(envVars.filter((_, j) => j !== i))}
                        className="text-text-tertiary hover:text-error transition-colors text-xs shrink-0"
                      >
                        &times;
                      </button>
                    </div>
                  ))}
                </div>
              </div>
            )}
          </DialogBody>

          <DialogFooter>
            <Button type="button" variant="secondary" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={!image.trim() || createSandbox.isPending}>
              {createSandbox.isPending ? (
                <>
                  <Loader2 className="w-3.5 h-3.5 animate-spin" />
                  Creating...
                </>
              ) : (
                'Create Sandbox'
              )}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
