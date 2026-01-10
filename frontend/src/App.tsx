import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Layout } from '@/components/layout/Layout';
import { Dashboard } from '@/components/dashboard/Dashboard';
import { SandboxList } from '@/components/sandbox/SandboxList';
import { SandboxDetail } from '@/components/sandbox/SandboxDetail';
import { Settings } from '@/components/Settings';
import { TemplatesPage, NetworkPage, ActivityPage } from '@/components/PlaceholderPages';

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 5000,
      retry: 1,
    },
  },
});

function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <Layout>
          <Routes>
            <Route path="/" element={<Dashboard />} />
            <Route path="/sandboxes" element={<SandboxList />} />
            <Route path="/sandboxes/:id" element={<SandboxDetail />} />
            <Route path="/templates" element={<TemplatesPage />} />
            <Route path="/network" element={<NetworkPage />} />
            <Route path="/activity" element={<ActivityPage />} />
            <Route path="/settings" element={<Settings />} />
          </Routes>
        </Layout>
      </BrowserRouter>
    </QueryClientProvider>
  );
}

export default App;
