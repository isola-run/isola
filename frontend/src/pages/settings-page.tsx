import { useState } from 'react'
import { useSettingsStore } from '@/stores/settings-store'
import { Input, Label } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { toast } from 'sonner'
import { Save, RotateCcw, Server, Palette } from 'lucide-react'
import { cn } from '@/lib/utils'

export function SettingsPage() {
  const { apiBaseUrl, theme, setApiBaseUrl, setTheme } = useSettingsStore()
  const [url, setUrl] = useState(apiBaseUrl)

  const handleSave = () => {
    setApiBaseUrl(url)
    toast.success('Settings saved')
  }

  const handleReset = () => {
    setUrl('/api')
    setApiBaseUrl('/api')
    toast.success('Settings reset to defaults')
  }

  return (
    <div className="max-w-2xl mx-auto px-6 py-8 animate-fade-in">
      <div className="mb-8">
        <h1 className="text-xl font-semibold text-text-primary mb-1">Settings</h1>
        <p className="text-sm text-text-secondary">Configure your Isola dashboard</p>
      </div>

      <div className="space-y-6">
        {/* API Configuration */}
        <div className="bg-bg-surface border border-border-subtle rounded-xl p-5">
          <div className="flex items-center gap-2 mb-4">
            <Server className="w-4 h-4 text-accent" />
            <h2 className="text-sm font-semibold text-text-primary">API Configuration</h2>
          </div>

          <div className="space-y-4">
            <div className="space-y-1.5">
              <Label htmlFor="api-url">API Base URL</Label>
              <Input
                id="api-url"
                value={url}
                onChange={(e) => setUrl(e.target.value)}
                placeholder="/api or http://localhost:8080"
                className="font-mono text-xs"
              />
              <p className="text-xs text-text-tertiary">
                The base URL for the Isola API gateway. Use <code className="font-mono">/api</code> for local development with proxy.
              </p>
            </div>

            <div className="flex items-center gap-2">
              <Button onClick={handleSave} size="sm">
                <Save className="w-3.5 h-3.5" />
                Save
              </Button>
              <Button variant="secondary" size="sm" onClick={handleReset}>
                <RotateCcw className="w-3.5 h-3.5" />
                Reset
              </Button>
            </div>
          </div>
        </div>

        {/* Appearance */}
        <div className="bg-bg-surface border border-border-subtle rounded-xl p-5">
          <div className="flex items-center gap-2 mb-4">
            <Palette className="w-4 h-4 text-accent" />
            <h2 className="text-sm font-semibold text-text-primary">Appearance</h2>
          </div>

          <div className="space-y-3">
            <Label>Theme</Label>
            <div className="grid grid-cols-3 gap-2">
              {(['dark', 'light', 'system'] as const).map((t) => (
                <button
                  key={t}
                  onClick={() => setTheme(t)}
                  className={cn(
                    'px-3 py-2 rounded-lg text-sm font-medium transition-colors border',
                    theme === t
                      ? 'bg-accent/10 border-accent/30 text-accent'
                      : 'bg-bg-input border-border-default text-text-secondary hover:text-text-primary hover:bg-bg-hover',
                  )}
                >
                  {t.charAt(0).toUpperCase() + t.slice(1)}
                </button>
              ))}
            </div>
          </div>
        </div>

        {/* About */}
        <div className="bg-bg-surface border border-border-subtle rounded-xl p-5">
          <h2 className="text-sm font-semibold text-text-primary mb-3">About Isola</h2>
          <div className="space-y-2 text-sm text-text-secondary">
            <p>
              Isola is a sandbox orchestration platform for running isolated container environments
              with real-time command execution, file management, and network isolation.
            </p>
            <div className="flex items-center gap-2 pt-2 border-t border-border-subtle mt-3">
              <span className="text-xs text-text-tertiary">Frontend v1.0.0</span>
              <span className="text-text-tertiary">&middot;</span>
              <span className="text-xs text-text-tertiary">API: {apiBaseUrl}</span>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}
