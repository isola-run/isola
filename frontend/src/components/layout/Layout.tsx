import { type ReactNode, useState } from 'react';
import { Header } from './Header';
import { Sidebar } from './Sidebar';
import { CreateSandboxModal } from '@/components/sandbox/CreateSandboxModal';

interface LayoutProps {
  children: ReactNode;
}

function Layout({ children }: LayoutProps) {
  const [isCreateModalOpen, setIsCreateModalOpen] = useState(false);

  return (
    <div className="min-h-screen bg-background">
      <Header />
      <Sidebar onCreateClick={() => setIsCreateModalOpen(true)} />

      {/* Main content */}
      <main className="lg:pl-64">
        <div className="container mx-auto p-6">{children}</div>
      </main>

      {/* Create Sandbox Modal */}
      <CreateSandboxModal
        isOpen={isCreateModalOpen}
        onClose={() => setIsCreateModalOpen(false)}
      />
    </div>
  );
}

export { Layout };
