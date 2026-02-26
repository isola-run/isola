import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import { resetApiClient } from '@/api/client'

interface SettingsState {
  apiBaseUrl: string
  theme: 'dark' | 'light' | 'system'
  setApiBaseUrl: (url: string) => void
  setTheme: (theme: 'dark' | 'light' | 'system') => void
}

export const useSettingsStore = create<SettingsState>()(
  persist(
    (set) => ({
      apiBaseUrl: '/api',
      theme: 'dark',
      setApiBaseUrl: (url) => {
        localStorage.setItem('isola-api-url', url)
        resetApiClient()
        set({ apiBaseUrl: url })
      },
      setTheme: (theme) => set({ theme }),
    }),
    { name: 'isola-settings' },
  ),
)
