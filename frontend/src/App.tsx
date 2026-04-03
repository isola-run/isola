import { Routes, Route, Navigate } from "react-router-dom";
import Layout from "./components/Layout";
import ErrorBoundary from "./components/ErrorBoundary";
import { ToastProvider } from "./components/Toast";
import SandboxListPage from "./pages/SandboxListPage";
import SandboxDetailPage from "./pages/SandboxDetailPage";
import CreateSandboxPage from "./pages/CreateSandboxPage";

export default function App() {
  return (
    <ErrorBoundary>
      <ToastProvider>
        <Layout>
          <Routes>
            <Route path="/" element={<Navigate to="/sandboxes" replace />} />
            <Route path="/sandboxes" element={<SandboxListPage />} />
            <Route path="/sandboxes/new" element={<CreateSandboxPage />} />
            <Route path="/sandboxes/:id" element={<SandboxDetailPage />} />
          </Routes>
        </Layout>
      </ToastProvider>
    </ErrorBoundary>
  );
}
