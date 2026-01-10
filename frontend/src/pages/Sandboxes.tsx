import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useSearchParams } from 'react-router-dom'
import {
  Plus,
  Search,
  Filter,
  Box,
  LayoutGrid,
  List,
  RefreshCw,
} from 'lucide-react'
import { Header } from '@/components/layout'
import {
  Card,
  Button,
  Input,
  Select,
  LoadingOverlay,
  EmptyState,
  Badge,
} from '@/components/ui'
import { SandboxCard, SandboxStatusBadge, CreateSandboxModal } from '@/components/sandbox'
import { apiClient } from '@/api/client'
import { formatDistanceToNow } from '@/lib/utils'
import { clsx } from 'clsx'
import type { SandboxState, Sandbox } from '@/types'

const stateOptions = [
  { value: '', label: 'All States' },
  { value: 'running', label: 'Running' },
  { value: 'pending', label: 'Pending' },
  { value: 'starting', label: 'Starting' },
  { value: 'stopped', label: 'Stopped' },
  { value: 'error', label: 'Error' },
]

type ViewMode = 'grid' | 'list'

export function Sandboxes() {
  const [searchParams, setSearchParams] = useSearchParams()
  const [isCreateModalOpen, setIsCreateModalOpen] = useState(false)
  const [viewMode, setViewMode] = useState<ViewMode>('grid')
  const [searchQuery, setSearchQuery] = useState('')

  const stateFilter = searchParams.get('state') as SandboxState | null

  const {
    data: sandboxes,
    isLoading,
    error,
    refetch,
    isFetching,
  } = useQuery({
    queryKey: ['sandboxes', stateFilter],
    queryFn: () =>
      apiClient.listSandboxes({
        state: stateFilter || undefined,
        limit: 100,
      }),
    refetchInterval: 5000,
  })

  const filteredItems = (sandboxes?.items || []).filter((sandbox) => {
    if (!searchQuery) return true
    const query = searchQuery.toLowerCase()
    return (
      sandbox.name.toLowerCase().includes(query) ||
      sandbox.id.toLowerCase().includes(query)
    )
  })

  const handleStateChange = (state: string) => {
    if (state) {
      setSearchParams({ state })
    } else {
      setSearchParams({})
    }
  }

  if (error) {
    return (
      <EmptyState
        icon={<Box className="h-8 w-8" />}
        title="Failed to load sandboxes"
        description={(error as Error).message}
        action={
          <Button onClick={() => refetch()} variant="outline">
            Try Again
          </Button>
        }
      />
    )
  }

  return (
    <div>
      <Header
        title="Sandboxes"
        description={`${sandboxes?.total || 0} sandbox${(sandboxes?.total || 0) !== 1 ? 'es' : ''} total`}
        actions={
          <Button
            leftIcon={<Plus className="h-4 w-4" />}
            onClick={() => setIsCreateModalOpen(true)}
          >
            Create Sandbox
          </Button>
        }
      />

      {/* Filters */}
      <Card className="mb-6">
        <div className="flex flex-col sm:flex-row gap-4">
          <div className="flex-1">
            <Input
              placeholder="Search by name or ID..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              leftIcon={<Search className="h-4 w-4" />}
            />
          </div>
          <div className="flex gap-3">
            <Select
              options={stateOptions}
              value={stateFilter || ''}
              onChange={(e) => handleStateChange(e.target.value)}
              className="w-40"
            />
            <Button
              variant="outline"
              size="md"
              onClick={() => refetch()}
              isLoading={isFetching}
              leftIcon={<RefreshCw className="h-4 w-4" />}
            >
              Refresh
            </Button>
            <div className="flex border border-slate-300 rounded-lg overflow-hidden">
              <button
                onClick={() => setViewMode('grid')}
                className={clsx(
                  'p-2 transition-colors',
                  viewMode === 'grid'
                    ? 'bg-slate-100 text-slate-900'
                    : 'text-slate-500 hover:text-slate-700'
                )}
              >
                <LayoutGrid className="h-5 w-5" />
              </button>
              <button
                onClick={() => setViewMode('list')}
                className={clsx(
                  'p-2 transition-colors',
                  viewMode === 'list'
                    ? 'bg-slate-100 text-slate-900'
                    : 'text-slate-500 hover:text-slate-700'
                )}
              >
                <List className="h-5 w-5" />
              </button>
            </div>
          </div>
        </div>

        {/* Active filters */}
        {stateFilter && (
          <div className="flex items-center gap-2 mt-4 pt-4 border-t border-slate-100">
            <Filter className="h-4 w-4 text-slate-400" />
            <span className="text-sm text-slate-500">Filters:</span>
            <Badge variant="default">
              State: {stateFilter}
              <button
                onClick={() => handleStateChange('')}
                className="ml-1 hover:text-primary-800"
              >
                &times;
              </button>
            </Badge>
          </div>
        )}
      </Card>

      {/* Content */}
      {isLoading ? (
        <LoadingOverlay message="Loading sandboxes..." />
      ) : filteredItems.length === 0 ? (
        <Card>
          <EmptyState
            icon={<Box className="h-8 w-8" />}
            title={searchQuery ? 'No matching sandboxes' : 'No sandboxes yet'}
            description={
              searchQuery
                ? 'Try adjusting your search or filters'
                : 'Create your first sandbox to get started'
            }
            action={
              !searchQuery && (
                <Button
                  leftIcon={<Plus className="h-4 w-4" />}
                  onClick={() => setIsCreateModalOpen(true)}
                >
                  Create Sandbox
                </Button>
              )
            }
          />
        </Card>
      ) : viewMode === 'grid' ? (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {filteredItems.map((sandbox) => (
            <SandboxCard key={sandbox.id} sandbox={sandbox} />
          ))}
        </div>
      ) : (
        <Card padding="none">
          <table className="w-full">
            <thead>
              <tr className="border-b border-slate-200 bg-slate-50">
                <th className="px-6 py-3 text-left text-xs font-medium text-slate-500 uppercase tracking-wider">
                  Name
                </th>
                <th className="px-6 py-3 text-left text-xs font-medium text-slate-500 uppercase tracking-wider">
                  Status
                </th>
                <th className="px-6 py-3 text-left text-xs font-medium text-slate-500 uppercase tracking-wider">
                  Created
                </th>
                <th className="px-6 py-3 text-left text-xs font-medium text-slate-500 uppercase tracking-wider">
                  Labels
                </th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-100">
              {filteredItems.map((sandbox: Sandbox) => (
                <tr
                  key={sandbox.id}
                  className="hover:bg-slate-50 cursor-pointer transition-colors"
                  onClick={() => (window.location.href = `/sandboxes/${sandbox.id}`)}
                >
                  <td className="px-6 py-4">
                    <div>
                      <p className="font-medium text-slate-900">{sandbox.name}</p>
                      <p className="text-xs text-slate-400 font-mono">
                        {sandbox.id}
                      </p>
                    </div>
                  </td>
                  <td className="px-6 py-4">
                    <SandboxStatusBadge state={sandbox.state} />
                  </td>
                  <td className="px-6 py-4 text-sm text-slate-500">
                    {formatDistanceToNow(sandbox.createdAt)}
                  </td>
                  <td className="px-6 py-4">
                    <div className="flex gap-1 flex-wrap">
                      {Object.entries(sandbox.labels || {})
                        .slice(0, 3)
                        .map(([key, value]) => (
                          <Badge key={key} variant="neutral" size="sm">
                            {key}: {value}
                          </Badge>
                        ))}
                      {Object.keys(sandbox.labels || {}).length > 3 && (
                        <Badge variant="neutral" size="sm">
                          +{Object.keys(sandbox.labels || {}).length - 3}
                        </Badge>
                      )}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </Card>
      )}

      <CreateSandboxModal
        isOpen={isCreateModalOpen}
        onClose={() => setIsCreateModalOpen(false)}
      />
    </div>
  )
}
