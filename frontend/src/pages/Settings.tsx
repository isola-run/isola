import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Key, Eye, EyeOff, Check, AlertCircle, Activity } from 'lucide-react'
import { Header } from '@/components/layout'
import { Card, CardHeader, CardTitle, CardContent, Button, Input } from '@/components/ui'
import { useAuth } from '@/context/AuthContext'
import { apiClient } from '@/api/client'

export function Settings() {
  const { apiKey, setApiKey, isAuthenticated } = useAuth()
  const [inputKey, setInputKey] = useState(apiKey || '')
  const [showKey, setShowKey] = useState(false)
  const [saved, setSaved] = useState(false)

  const {
    data: health,
    isLoading: isHealthLoading,
    error: healthError,
    refetch: refetchHealth,
  } = useQuery({
    queryKey: ['health'],
    queryFn: () => apiClient.health(),
    retry: false,
  })

  const handleSave = () => {
    setApiKey(inputKey || null)
    setSaved(true)
    setTimeout(() => setSaved(false), 2000)
    refetchHealth()
  }

  const handleClear = () => {
    setInputKey('')
    setApiKey(null)
  }

  return (
    <div>
      <Header
        title="Settings"
        description="Configure your Isola environment"
      />

      <div className="space-y-6 max-w-2xl">
        {/* API Key Configuration */}
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Key className="h-5 w-5 text-slate-400" />
              API Key
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <p className="text-sm text-slate-600">
              Your API key is used to authenticate requests to the Isola gateway.
              Keep this key secure and never share it publicly.
            </p>

            <div className="flex gap-3">
              <div className="flex-1 relative">
                <Input
                  type={showKey ? 'text' : 'password'}
                  value={inputKey}
                  onChange={(e) => setInputKey(e.target.value)}
                  placeholder="iso_sk_..."
                  className="font-mono pr-10"
                />
                <button
                  type="button"
                  onClick={() => setShowKey(!showKey)}
                  className="absolute right-3 top-1/2 -translate-y-1/2 text-slate-400 hover:text-slate-600"
                >
                  {showKey ? (
                    <EyeOff className="h-4 w-4" />
                  ) : (
                    <Eye className="h-4 w-4" />
                  )}
                </button>
              </div>
              <Button onClick={handleSave} disabled={saved}>
                {saved ? (
                  <>
                    <Check className="h-4 w-4 mr-1" />
                    Saved
                  </>
                ) : (
                  'Save'
                )}
              </Button>
              {isAuthenticated && (
                <Button variant="outline" onClick={handleClear}>
                  Clear
                </Button>
              )}
            </div>

            <div className="flex items-center gap-2 text-sm">
              <span className="text-slate-500">Status:</span>
              {isAuthenticated ? (
                <span className="text-emerald-600 flex items-center gap-1">
                  <span className="h-2 w-2 bg-emerald-500 rounded-full" />
                  Configured
                </span>
              ) : (
                <span className="text-amber-600 flex items-center gap-1">
                  <span className="h-2 w-2 bg-amber-500 rounded-full" />
                  Not configured
                </span>
              )}
            </div>
          </CardContent>
        </Card>

        {/* System Status */}
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Activity className="h-5 w-5 text-slate-400" />
              System Status
            </CardTitle>
          </CardHeader>
          <CardContent>
            {isHealthLoading ? (
              <div className="flex items-center gap-2 text-sm text-slate-500">
                <div className="h-4 w-4 border-2 border-slate-300 border-t-transparent rounded-full animate-spin" />
                Checking system health...
              </div>
            ) : healthError ? (
              <div className="flex items-start gap-2 p-3 bg-red-50 rounded-lg">
                <AlertCircle className="h-5 w-5 text-red-500 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="text-sm font-medium text-red-800">
                    Unable to connect to gateway
                  </p>
                  <p className="text-sm text-red-700 mt-0.5">
                    {(healthError as Error).message}
                  </p>
                </div>
              </div>
            ) : health ? (
              <div className="space-y-4">
                <div className="flex items-center justify-between p-3 bg-emerald-50 rounded-lg">
                  <div className="flex items-center gap-2">
                    <span className="h-3 w-3 bg-emerald-500 rounded-full animate-pulse-subtle" />
                    <span className="text-sm font-medium text-emerald-800">
                      System Healthy
                    </span>
                  </div>
                  <span className="text-sm text-emerald-600">v{health.version}</span>
                </div>

                <div className="grid grid-cols-2 gap-4">
                  <div>
                    <p className="text-xs text-slate-500 mb-1">Status</p>
                    <p className="text-sm font-medium text-slate-900 capitalize">
                      {health.status}
                    </p>
                  </div>
                  <div>
                    <p className="text-xs text-slate-500 mb-1">Last Check</p>
                    <p className="text-sm font-medium text-slate-900">
                      {new Date(health.timestamp).toLocaleTimeString()}
                    </p>
                  </div>
                </div>

                {Object.keys(health.components || {}).length > 0 && (
                  <div>
                    <p className="text-xs text-slate-500 mb-2">Components</p>
                    <div className="space-y-1">
                      {Object.entries(health.components).map(([name, status]) => (
                        <div
                          key={name}
                          className="flex items-center justify-between text-sm"
                        >
                          <span className="text-slate-700 capitalize">{name}</span>
                          <span
                            className={
                              status === 'healthy'
                                ? 'text-emerald-600'
                                : 'text-red-600'
                            }
                          >
                            {status}
                          </span>
                        </div>
                      ))}
                    </div>
                  </div>
                )}
              </div>
            ) : null}
          </CardContent>
        </Card>

        {/* About */}
        <Card>
          <CardHeader>
            <CardTitle>About Isola</CardTitle>
          </CardHeader>
          <CardContent className="prose prose-sm prose-slate">
            <p className="text-sm text-slate-600">
              Isola is a secure sandbox management platform that allows you to create,
              manage, and interact with isolated computing environments. Each sandbox
              runs in its own container with network isolation and resource limits.
            </p>
            <div className="mt-4 pt-4 border-t border-slate-100">
              <p className="text-xs text-slate-400">
                &copy; {new Date().getFullYear()} Isola. Built with React, TypeScript, and Tailwind CSS.
              </p>
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
