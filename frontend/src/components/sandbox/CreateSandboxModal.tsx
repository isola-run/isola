import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Modal, ModalFooter, Button, Input } from '@/components/ui'
import { apiClient } from '@/api/client'
import type { CreateSandboxRequest } from '@/types'

interface CreateSandboxModalProps {
  isOpen: boolean
  onClose: () => void
}

export function CreateSandboxModal({ isOpen, onClose }: CreateSandboxModalProps) {
  const queryClient = useQueryClient()
  const [formData, setFormData] = useState<CreateSandboxRequest>({
    name: '',
    image: 'ubuntu:latest',
    cpu: 0.5,
    memory: 0.5,
    autoStart: true,
  })
  const [envVars, setEnvVars] = useState<Array<{ key: string; value: string }>>([])
  const [showAdvanced, setShowAdvanced] = useState(false)

  const createMutation = useMutation({
    mutationFn: (data: CreateSandboxRequest) => apiClient.createSandbox(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['sandboxes'] })
      handleClose()
    },
  })

  const handleClose = () => {
    setFormData({
      name: '',
      image: 'ubuntu:latest',
      cpu: 0.5,
      memory: 0.5,
      autoStart: true,
    })
    setEnvVars([])
    setShowAdvanced(false)
    createMutation.reset()
    onClose()
  }

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()

    const env = envVars.reduce(
      (acc, { key, value }) => {
        if (key.trim()) {
          acc[key.trim()] = value
        }
        return acc
      },
      {} as Record<string, string>
    )

    createMutation.mutate({
      ...formData,
      env: Object.keys(env).length > 0 ? env : undefined,
    })
  }

  const addEnvVar = () => {
    setEnvVars((prev) => [...prev, { key: '', value: '' }])
  }

  const updateEnvVar = (
    index: number,
    field: 'key' | 'value',
    value: string
  ) => {
    setEnvVars((prev) =>
      prev.map((v, i) => (i === index ? { ...v, [field]: value } : v))
    )
  }

  const removeEnvVar = (index: number) => {
    setEnvVars((prev) => prev.filter((_, i) => i !== index))
  }

  return (
    <Modal
      isOpen={isOpen}
      onClose={handleClose}
      title="Create Sandbox"
      description="Launch a new isolated sandbox environment"
      size="lg"
    >
      <form onSubmit={handleSubmit}>
        <div className="space-y-4">
          {/* Name */}
          <Input
            label="Name"
            value={formData.name}
            onChange={(e) =>
              setFormData((prev) => ({ ...prev, name: e.target.value }))
            }
            placeholder="my-sandbox"
            required
          />

          {/* Image */}
          <Input
            label="Container Image"
            value={formData.image || ''}
            onChange={(e) =>
              setFormData((prev) => ({ ...prev, image: e.target.value }))
            }
            placeholder="ubuntu:latest"
            hint="Docker image to use for the sandbox"
          />

          {/* Auto Start */}
          <label className="flex items-center gap-3 cursor-pointer">
            <input
              type="checkbox"
              checked={formData.autoStart}
              onChange={(e) =>
                setFormData((prev) => ({ ...prev, autoStart: e.target.checked }))
              }
              className="h-4 w-4 rounded border-slate-300 text-primary-600 focus:ring-primary-500"
            />
            <span className="text-sm text-slate-700">
              Start sandbox immediately after creation
            </span>
          </label>

          {/* Advanced Options Toggle */}
          <button
            type="button"
            onClick={() => setShowAdvanced(!showAdvanced)}
            className="text-sm text-primary-600 hover:text-primary-700 font-medium"
          >
            {showAdvanced ? 'Hide' : 'Show'} advanced options
          </button>

          {showAdvanced && (
            <div className="space-y-4 pt-2 border-t border-slate-100">
              {/* Resources */}
              <div className="grid grid-cols-2 gap-4">
                <Input
                  label="CPU (cores)"
                  type="number"
                  step="0.1"
                  min="0.1"
                  value={formData.cpu || ''}
                  onChange={(e) =>
                    setFormData((prev) => ({
                      ...prev,
                      cpu: parseFloat(e.target.value) || undefined,
                    }))
                  }
                  placeholder="0.5"
                />
                <Input
                  label="Memory (GB)"
                  type="number"
                  step="0.1"
                  min="0.1"
                  value={formData.memory || ''}
                  onChange={(e) =>
                    setFormData((prev) => ({
                      ...prev,
                      memory: parseFloat(e.target.value) || undefined,
                    }))
                  }
                  placeholder="0.5"
                />
              </div>

              {/* Environment Variables */}
              <div>
                <label className="block text-sm font-medium text-slate-700 mb-2">
                  Environment Variables
                </label>
                <div className="space-y-2">
                  {envVars.map((v, i) => (
                    <div key={i} className="flex gap-2">
                      <Input
                        placeholder="KEY"
                        value={v.key}
                        onChange={(e) => updateEnvVar(i, 'key', e.target.value)}
                        className="flex-1"
                      />
                      <Input
                        placeholder="value"
                        value={v.value}
                        onChange={(e) =>
                          updateEnvVar(i, 'value', e.target.value)
                        }
                        className="flex-1"
                      />
                      <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        onClick={() => removeEnvVar(i)}
                        className="text-slate-400 hover:text-red-500"
                      >
                        Remove
                      </Button>
                    </div>
                  ))}
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={addEnvVar}
                  >
                    Add Variable
                  </Button>
                </div>
              </div>
            </div>
          )}

          {createMutation.error && (
            <p className="text-sm text-red-600">
              {(createMutation.error as Error).message}
            </p>
          )}
        </div>

        <ModalFooter>
          <Button type="button" variant="secondary" onClick={handleClose}>
            Cancel
          </Button>
          <Button
            type="submit"
            isLoading={createMutation.isPending}
            disabled={!formData.name.trim()}
          >
            Create Sandbox
          </Button>
        </ModalFooter>
      </form>
    </Modal>
  )
}
