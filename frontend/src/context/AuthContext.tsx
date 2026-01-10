import { createContext, useContext, useState, useCallback, useEffect, type ReactNode } from 'react'
import { apiClient } from '@/api/client'

interface AuthContextType {
  apiKey: string | null
  isAuthenticated: boolean
  setApiKey: (key: string | null) => void
  logout: () => void
}

const AuthContext = createContext<AuthContextType | undefined>(undefined)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [apiKey, setApiKeyState] = useState<string | null>(() => {
    return localStorage.getItem('isola_api_key')
  })

  const setApiKey = useCallback((key: string | null) => {
    setApiKeyState(key)
    apiClient.setApiKey(key)
  }, [])

  const logout = useCallback(() => {
    setApiKey(null)
  }, [setApiKey])

  useEffect(() => {
    // Initialize API client with stored key
    const storedKey = localStorage.getItem('isola_api_key')
    if (storedKey) {
      apiClient.setApiKey(storedKey)
    }
  }, [])

  return (
    <AuthContext.Provider
      value={{
        apiKey,
        isAuthenticated: !!apiKey,
        setApiKey,
        logout,
      }}
    >
      {children}
    </AuthContext.Provider>
  )
}

export function useAuth() {
  const context = useContext(AuthContext)
  if (context === undefined) {
    throw new Error('useAuth must be used within an AuthProvider')
  }
  return context
}
