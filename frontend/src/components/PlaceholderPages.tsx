import { FileText, Shield, Activity, Construction } from 'lucide-react';
import { Card, CardContent } from '@/components/ui/Card';

interface PlaceholderPageProps {
  title: string;
  description: string;
  icon: React.ComponentType<{ className?: string }>;
}

function PlaceholderPage({ title, description, icon: Icon }: PlaceholderPageProps) {
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold tracking-tight">{title}</h1>
        <p className="text-muted-foreground mt-1">{description}</p>
      </div>

      <Card>
        <CardContent className="flex flex-col items-center justify-center py-16">
          <div className="flex h-16 w-16 items-center justify-center rounded-full bg-muted">
            <Icon className="h-8 w-8 text-muted-foreground" />
          </div>
          <div className="mt-4 flex items-center gap-2 text-muted-foreground">
            <Construction className="h-4 w-4" />
            <span>Coming Soon</span>
          </div>
          <p className="mt-2 text-sm text-muted-foreground text-center max-w-md">
            This feature is currently under development. Check back later for updates.
          </p>
        </CardContent>
      </Card>
    </div>
  );
}

function TemplatesPage() {
  return (
    <PlaceholderPage
      title="Templates"
      description="Manage sandbox templates for quick deployment"
      icon={FileText}
    />
  );
}

function NetworkPage() {
  return (
    <PlaceholderPage
      title="Network"
      description="Configure network policies and security rules"
      icon={Shield}
    />
  );
}

function ActivityPage() {
  return (
    <PlaceholderPage
      title="Activity"
      description="View logs and activity across your sandboxes"
      icon={Activity}
    />
  );
}

export { TemplatesPage, NetworkPage, ActivityPage };
