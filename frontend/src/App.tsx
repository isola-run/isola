import { Routes, Route, Navigate } from 'react-router-dom'
import { Layout } from '@/components/layout'
import { AuthProvider } from '@/context/AuthContext'
import { Dashboard, Sandboxes, SandboxDetail, Settings, Login } from '@/pages'

function AppRoutes() {
  return (
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route
        path="/*"
        element={
          <Layout>
            <Routes>
              <Route path="/" element={<Dashboard />} />
              <Route path="/sandboxes" element={<Sandboxes />} />
              <Route path="/sandboxes/:id" element={<SandboxDetail />} />
              <Route path="/settings" element={<Settings />} />
              <Route path="*" element={<Navigate to="/" replace />} />
            </Routes>
          </Layout>
        }
      />
    </Routes>
  )
}

function App() {
  return (
    <AuthProvider>
      <AppRoutes />
    </AuthProvider>
  )
}

export default App
