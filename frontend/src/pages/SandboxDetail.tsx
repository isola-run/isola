import { useState } from 'react'
import { useParams, useNavigate, Link } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  ArrowLeft,
  Trash2,
  Copy,
  Check,
  Clock,
  Tag,
  Terminal as TerminalIcon,
  Upload,
  Settings,
  AlertCircle,
} from 'lucide-react'
import { Header } from '@/components/layout'
import {
  Card,
  CardHeader,
  CardTitle,
  CardContent,
  Button,
  Badge,
  LoadingOverlay,
  EmptyState,
  Modal,
  ModalFooter,
} from '@/components/ui'
import { SandboxStatusBadge, Terminal, FileUpload } from '@/components/sandbox'
import { apiClient } from '@/api/client'
import { formatDate, formatDistanceToNow } from '@/lib/utils'
import { clsx } from 'clsx'

type Tab = 'terminal' | 'files' | 'settings'

export function SandboxDetail() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const queryClient = useQueryClient()

  const [activeTab, setActiveTab] = useState<Tab>('terminal')
  const [copied, setCopied] = useState(false)
  const [isDeleteModalOpen, setIsDeleteModalOpen] = useState(false)

  const {
    data: sandbox,
    isLoading,
    error,
  } = useQuery({
    queryKey: ['sandbox', id],
    queryFn: () => apiClient.getSandbox(id!),
    enabled: !!id,
    refetchInterval: 3000,
  })

  const deleteMutation = useMutation({
    mutationFn: () => apiClient.deleteSandbox(id!),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['sandboxes'] })
      navigate('/sandboxes')
    },
  })

  const copyId = async () => {
    if (sandbox?.id) {
      await navigator.clipboard.writeText(sandbox.id)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    }
  }

  if (isLoading) {
    return <LoadingOverlay message="Loading sandbox..." />
  }

  if (error || !sandbox) {
    return (
      <div>
        <Link
          to="/sandboxes"
          className="inline-flex items-center gap-2 text-sm text-slate-600 hover:text-slate-900 mb-4"
        >
          <ArrowLeft className="h-4 w-4" />
          Back to Sandboxes
        </Link>
        <EmptyState
          icon={<AlertCircle className="h-8 w-8" />}
          title="Sandbox not found"
          description={(error as Error)?.message || 'The sandbox you are looking for does not exist'}
          action={
            <Link to="/sandboxes">
              <Button variant="outline">Back to Sandboxes</Button>
            </Link>
          }
        />
      </div>
    )
  }

  const isRunning = sandbox.state === 'running'
  const tabs: { id: Tab; label: string; icon: React.ReactNode }[] = [
    { id: 'terminal', label: 'Terminal', icon: <TerminalIcon className="h-4 w-4" /> },
    { id: 'files', label: 'Files', icon: <Upload className="h-4 w-4" /> },
    { id: 'settings', label: 'Settings', icon: <Settings className="h-4 w-4" /> },
  ]

  return (
    <div>
      <Link
        to="/sandboxes"
        className="inline-flex items-center gap-2 text-sm text-slate-600 hover:text-slate-900 mb-4"
      >
        <ArrowLeft className="h-4 w-4" />
        Back to Sandboxes
      </Link>

      <Header
        title={sandbox.name}
        description={
          <button
            onClick={copyId}
            className="inline-flex items-center gap-1.5 text-sm text-slate-400 font-mono hover:text-slate-600 transition-colors"
          >
            {sandbox.id}
            {copied ? (
              <Check className="h-3.5 w-3.5 text-emerald-500" />
            ) : (
              <Copy className="h-3.5 w-3.5" />
            )}
          </button>
        }
        actions={
          <div className="flex items-center gap-3">
            <SandboxStatusBadge state={sandbox.state} />
            <Button
              variant="danger"
              leftIcon={<Trash2 className="h-4 w-4" />}
              onClick={() => setIsDeleteModalOpen(true)}
            >
              Delete
            </Button>
          </div>
        }
      />

      {/* Sandbox Info */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6 mb-6">
        <Card className="lg:col-span-2">
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-4">
            <div>
              <p className="text-xs text-slate-500 mb-1">State</p>
              <SandboxStatusBadge state={sandbox.state} />
            </div>
            {sandbox.desiredState && (
              <div>
                <p className="text-xs text-slate-500 mb-1">Desired State</p>
                <Badge variant="neutral">{sandbox.desiredState}</Badge>
              </div>
            )}
            <div>
              <p className="text-xs text-slate-500 mb-1">Created</p>
              <div className="flex items-center gap-1.5 text-sm text-slate-700">
                <Clock className="h-3.5 w-3.5 text-slate-400" />
                {formatDistanceToNow(sandbox.createdAt)}
              </div>
            </div>
            <div>
              <p className="text-xs text-slate-500 mb-1">Created At</p>
              <p className="text-sm text-slate-700">
                {formatDate(sandbox.createdAt)}
              </p>
            </div>
          </div>

          {sandbox.errorReason && (
            <div className="mt-4 p-3 bg-red-50 border border-red-100 rounded-lg">
              <div className="flex items-start gap-2">
                <AlertCircle className="h-5 w-5 text-red-500 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="text-sm font-medium text-red-800">Error</p>
                  <p className="text-sm text-red-700 mt-0.5">
                    {sandbox.errorReason}
                  </p>
                </div>
              </div>
            </div>
          )}
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base flex items-center gap-2">
              <Tag className="h-4 w-4 text-slate-400" />
              Labels
            </CardTitle>
          </CardHeader>
          <CardContent>
            {Object.keys(sandbox.labels || {}).length > 0 ? (
              <div className="flex flex-wrap gap-2">
                {Object.entries(sandbox.labels || {}).map(([key, value]) => (
                  <Badge key={key} variant="neutral">
                    {key}: {value}
                  </Badge>
                ))}
              </div>
            ) : (
              <p className="text-sm text-slate-500">No labels</p>
            )}
          </CardContent>
        </Card>
      </div>

      {/* Tabs */}
      <Card padding="none">
        {/* Tab Navigation */}
        <div className="flex border-b border-slate-200">
          {tabs.map((tab) => (
            <button
              key={tab.id}
              onClick={() => setActiveTab(tab.id)}
              className={clsx(
                'flex items-center gap-2 px-6 py-4 text-sm font-medium border-b-2 -mb-px transition-colors',
                activeTab === tab.id
                  ? 'border-primary-500 text-primary-600'
                  : 'border-transparent text-slate-500 hover:text-slate-700 hover:border-slate-300'
              )}
            >
              {tab.icon}
              {tab.label}
            </button>
          ))}
        </div>

        {/* Tab Content */}
        <div className="p-6">
          {activeTab === 'terminal' && (
            <div>
              {!isRunning && (
                <div className="mb-4 p-3 bg-amber-50 border border-amber-100 rounded-lg">
                  <p className="text-sm text-amber-800">
                    Terminal is only available when the sandbox is running.
                    Current state: <strong>{sandbox.state}</strong>
                  </p>
                </div>
              )}
              <Terminal sandboxId={sandbox.id} disabled={!isRunning} />
            </div>
          )}

          {activeTab === 'files' && (
            <div>
              {!isRunning && (
                <div className="mb-4 p-3 bg-amber-50 border border-amber-100 rounded-lg">
                  <p className="text-sm text-amber-800">
                    File upload is only available when the sandbox is running.
                    Current state: <strong>{sandbox.state}</strong>
                  </p>
                </div>
              )}
              <FileUpload sandboxId={sandbox.id} disabled={!isRunning} />
            </div>
          )}

          {activeTab === 'settings' && (
            <div className="space-y-6">
              {/* Environment Variables */}
              <div>
                <h3 className="text-sm font-medium text-slate-900 mb-3">
                  Environment Variables
                </h3>
                {Object.keys(sandbox.env || {}).length > 0 ? (
                  <div className="bg-slate-50 rounded-lg p-4 font-mono text-sm">
                    {Object.entries(sandbox.env || {}).map(([key, value]) => (
                      <div key={key} className="flex gap-2">
                        <span className="text-primary-600">{key}</span>
                        <span className="text-slate-400">=</span>
                        <span className="text-slate-700">{value}</span>
                      </div>
                    ))}
                  </div>
                ) : (
                  <p className="text-sm text-slate-500">
                    No environment variables configured
                  </p>
                )}
              </div>

              {/* Danger Zone */}
              <div className="pt-6 border-t border-slate-200">
                <h3 className="text-sm font-medium text-red-600 mb-3">
                  Danger Zone
                </h3>
                <Card className="border-red-200 bg-red-50">
                  <div className="flex items-center justify-between">
                    <div>
                      <p className="font-medium text-slate-900">Delete Sandbox</p>
                      <p className="text-sm text-slate-500 mt-0.5">
                        Permanently delete this sandbox and all its data
                      </p>
                    </div>
                    <Button
                      variant="danger"
                      onClick={() => setIsDeleteModalOpen(true)}
                    >
                      Delete
                    </Button>
                  </div>
                </Card>
              </div>
            </div>
          )}
        </div>
      </Card>

      {/* Delete Confirmation Modal */}
      <Modal
        isOpen={isDeleteModalOpen}
        onClose={() => setIsDeleteModalOpen(false)}
        title="Delete Sandbox"
        description="Are you sure you want to delete this sandbox? This action cannot be undone."
        size="sm"
      >
        <div className="space-y-4">
          <div className="p-3 bg-red-50 rounded-lg">
            <p className="text-sm text-red-700">
              Sandbox <strong>{sandbox.name}</strong> will be permanently deleted.
            </p>
          </div>

          {deleteMutation.error && (
            <p className="text-sm text-red-600">
              {(deleteMutation.error as Error).message}
            </p>
          )}
        </div>

        <ModalFooter>
          <Button
            variant="secondary"
            onClick={() => setIsDeleteModalOpen(false)}
          >
            Cancel
          </Button>
          <Button
            variant="danger"
            onClick={() => deleteMutation.mutate()}
            isLoading={deleteMutation.isPending}
          >
            Delete Sandbox
          </Button>
        </ModalFooter>
      </Modal>
    </div>
  )
}
