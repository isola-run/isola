import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Box, Key, ArrowRight } from 'lucide-react'
import { Card, Button, Input } from '@/components/ui'
import { useAuth } from '@/context/AuthContext'

export function Login() {
  const navigate = useNavigate()
  const { setApiKey } = useAuth()
  const [key, setKey] = useState('')
  const [error, setError] = useState('')

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    setError('')

    if (!key.trim()) {
      setError('API key is required')
      return
    }

    setApiKey(key.trim())
    navigate('/')
  }

  const handleSkip = () => {
    navigate('/')
  }

  return (
    <div className="min-h-screen bg-gradient-to-br from-slate-900 via-primary-950 to-slate-900 flex items-center justify-center p-4">
      <div className="w-full max-w-md">
        {/* Logo */}
        <div className="text-center mb-8">
          <div className="inline-flex items-center justify-center w-16 h-16 bg-gradient-to-br from-primary-500 to-accent-500 rounded-2xl mb-4 shadow-lg shadow-primary-500/25">
            <Box className="h-8 w-8 text-white" />
          </div>
          <h1 className="text-3xl font-bold text-white">Isola</h1>
          <p className="text-slate-400 mt-2">Secure Sandbox Management</p>
        </div>

        {/* Login Card */}
        <Card className="shadow-2xl">
          <form onSubmit={handleSubmit} className="space-y-6">
            <div>
              <h2 className="text-xl font-semibold text-slate-900 mb-1">
                Welcome
              </h2>
              <p className="text-sm text-slate-500">
                Enter your API key to access the dashboard
              </p>
            </div>

            <Input
              label="API Key"
              type="password"
              value={key}
              onChange={(e) => setKey(e.target.value)}
              placeholder="iso_sk_..."
              leftIcon={<Key className="h-4 w-4" />}
              error={error}
              autoFocus
            />

            <div className="space-y-3">
              <Button type="submit" className="w-full" rightIcon={<ArrowRight className="h-4 w-4" />}>
                Continue
              </Button>
              <Button
                type="button"
                variant="ghost"
                className="w-full"
                onClick={handleSkip}
              >
                Continue without API key
              </Button>
            </div>

            <p className="text-xs text-slate-500 text-center">
              Don't have an API key? Contact your administrator.
            </p>
          </form>
        </Card>

        {/* Footer */}
        <p className="text-center text-sm text-slate-500 mt-8">
          &copy; {new Date().getFullYear()} Isola. All rights reserved.
        </p>
      </div>
    </div>
  )
}
