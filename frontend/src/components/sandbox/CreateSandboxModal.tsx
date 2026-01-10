import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Plus, Minus, Info } from 'lucide-react';
import { Modal, ModalFooter } from '@/components/ui/Modal';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Select } from '@/components/ui/Select';
import { useCreateSandbox } from '@/hooks/useSandboxes';

interface CreateSandboxModalProps {
  isOpen: boolean;
  onClose: () => void;
}

const imageOptions = [
  { value: 'python:3.11', label: 'Python 3.11' },
  { value: 'python:3.12', label: 'Python 3.12' },
  { value: 'node:20', label: 'Node.js 20' },
  { value: 'node:18', label: 'Node.js 18' },
  { value: 'golang:1.21', label: 'Go 1.21' },
  { value: 'rust:1.75', label: 'Rust 1.75' },
  { value: 'ubuntu:22.04', label: 'Ubuntu 22.04' },
];

const cpuOptions = [
  { value: '0.5', label: '0.5 CPU' },
  { value: '1', label: '1 CPU' },
  { value: '2', label: '2 CPUs' },
  { value: '4', label: '4 CPUs' },
];

const memoryOptions = [
  { value: '0.5', label: '512 MB' },
  { value: '1', label: '1 GB' },
  { value: '2', label: '2 GB' },
  { value: '4', label: '4 GB' },
  { value: '8', label: '8 GB' },
];

function CreateSandboxModal({ isOpen, onClose }: CreateSandboxModalProps) {
  const navigate = useNavigate();
  const createMutation = useCreateSandbox();

  const [name, setName] = useState('');
  const [image, setImage] = useState('python:3.11');
  const [cpu, setCpu] = useState('1');
  const [memory, setMemory] = useState('1');
  const [autoStart, setAutoStart] = useState(true);
  const [envVars, setEnvVars] = useState<{ key: string; value: string }[]>([]);
  const [errors, setErrors] = useState<Record<string, string>>({});

  const resetForm = () => {
    setName('');
    setImage('python:3.11');
    setCpu('1');
    setMemory('1');
    setAutoStart(true);
    setEnvVars([]);
    setErrors({});
  };

  const handleClose = () => {
    resetForm();
    onClose();
  };

  const addEnvVar = () => {
    setEnvVars([...envVars, { key: '', value: '' }]);
  };

  const removeEnvVar = (index: number) => {
    setEnvVars(envVars.filter((_, i) => i !== index));
  };

  const updateEnvVar = (index: number, field: 'key' | 'value', value: string) => {
    const updated = [...envVars];
    updated[index][field] = value;
    setEnvVars(updated);
  };

  const validate = (): boolean => {
    const newErrors: Record<string, string> = {};

    if (!name.trim()) {
      newErrors.name = 'Name is required';
    } else if (!/^[a-z0-9][a-z0-9-]*[a-z0-9]$|^[a-z0-9]$/.test(name)) {
      newErrors.name = 'Name must be lowercase alphanumeric with optional hyphens';
    }

    // Check for duplicate env var keys
    const keys = envVars.map((e) => e.key).filter((k) => k);
    if (new Set(keys).size !== keys.length) {
      newErrors.env = 'Duplicate environment variable keys';
    }

    setErrors(newErrors);
    return Object.keys(newErrors).length === 0;
  };

  const handleSubmit = async () => {
    if (!validate()) return;

    const env: Record<string, string> = {};
    envVars.forEach(({ key, value }) => {
      if (key) env[key] = value;
    });

    try {
      const sandbox = await createMutation.mutateAsync({
        name: name.trim(),
        image,
        cpu: parseFloat(cpu),
        memory: parseFloat(memory),
        autoStart,
        env: Object.keys(env).length > 0 ? env : undefined,
      });

      handleClose();
      navigate(`/sandboxes/${sandbox.id}`);
    } catch (error) {
      setErrors({ submit: (error as Error).message });
    }
  };

  return (
    <Modal
      isOpen={isOpen}
      onClose={handleClose}
      title="Create New Sandbox"
      description="Configure your sandbox environment"
      size="lg"
    >
      <div className="space-y-4">
        {/* Name */}
        <Input
          label="Name"
          placeholder="my-sandbox"
          value={name}
          onChange={(e) => setName(e.target.value.toLowerCase())}
          error={errors.name}
          hint="Lowercase letters, numbers, and hyphens only"
        />

        {/* Image */}
        <Select
          label="Container Image"
          options={imageOptions}
          value={image}
          onChange={(e) => setImage(e.target.value)}
        />

        {/* Resources */}
        <div className="grid grid-cols-2 gap-4">
          <Select
            label="CPU"
            options={cpuOptions}
            value={cpu}
            onChange={(e) => setCpu(e.target.value)}
          />
          <Select
            label="Memory"
            options={memoryOptions}
            value={memory}
            onChange={(e) => setMemory(e.target.value)}
          />
        </div>

        {/* Auto Start */}
        <label className="flex items-center gap-3 cursor-pointer">
          <input
            type="checkbox"
            checked={autoStart}
            onChange={(e) => setAutoStart(e.target.checked)}
            className="h-4 w-4 rounded border-input text-primary focus:ring-primary"
          />
          <span className="text-sm font-medium">Start sandbox automatically</span>
        </label>

        {/* Environment Variables */}
        <div className="space-y-2">
          <div className="flex items-center justify-between">
            <label className="text-sm font-medium">Environment Variables</label>
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={addEnvVar}
            >
              <Plus className="mr-1 h-3 w-3" />
              Add
            </Button>
          </div>

          {envVars.length === 0 ? (
            <p className="text-sm text-muted-foreground py-2">
              No environment variables configured
            </p>
          ) : (
            <div className="space-y-2">
              {envVars.map((env, index) => (
                <div key={index} className="flex items-center gap-2">
                  <Input
                    placeholder="KEY"
                    value={env.key}
                    onChange={(e) => updateEnvVar(index, 'key', e.target.value.toUpperCase())}
                    className="flex-1 font-mono text-sm"
                  />
                  <span className="text-muted-foreground">=</span>
                  <Input
                    placeholder="value"
                    value={env.value}
                    onChange={(e) => updateEnvVar(index, 'value', e.target.value)}
                    className="flex-1 font-mono text-sm"
                  />
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    onClick={() => removeEnvVar(index)}
                    className="h-9 w-9 text-muted-foreground hover:text-destructive"
                  >
                    <Minus className="h-4 w-4" />
                  </Button>
                </div>
              ))}
            </div>
          )}
          {errors.env && <p className="text-xs text-destructive">{errors.env}</p>}
        </div>

        {/* Submit error */}
        {errors.submit && (
          <div className="p-3 rounded-lg bg-destructive/10 text-destructive text-sm flex items-start gap-2">
            <Info className="h-4 w-4 mt-0.5 flex-shrink-0" />
            {errors.submit}
          </div>
        )}
      </div>

      <ModalFooter>
        <Button variant="outline" onClick={handleClose}>
          Cancel
        </Button>
        <Button onClick={handleSubmit} isLoading={createMutation.isPending}>
          Create Sandbox
        </Button>
      </ModalFooter>
    </Modal>
  );
}

export { CreateSandboxModal };
