import { useState, useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'
import { Box, Plus, LayoutGrid, List, Search } from 'lucide-react'
import { useSandboxes } from '@/hooks/use-sandboxes'
import { SandboxCard } from '@/components/sandbox/sandbox-card'
import { SandboxTable } from '@/components/sandbox/sandbox-table'
import { CreateSandboxDialog } from '@/components/sandbox/create-sandbox-dialog'
import { DeleteSandboxDialog } from '@/components/sandbox/delete-sandbox-dialog'
import { EmptyState } from '@/components/ui/empty-state'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'
import type { SandboxStatus } from '@/api/types'

const statusFilters: Array<{ label: string; value: SandboxStatus | 'all' }> = [
  { label: 'All', value: 'all' },
  { label: 'Running', value: 'running' },
  { label: 'Creating', value: 'creating' },
  { label: 'Failed', value: 'failed' },
  { label: 'Stopped', value: 'stopped' },
]

export function SandboxesPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const { data: sandboxes, isLoading } = useSandboxes()

  const [search, setSearch] = useState('')
  const [statusFilter, setStatusFilter] = useState<SandboxStatus | 'all'>('all')
  const [viewMode, setViewMode] = useState<'grid' | 'table'>('table')
  const [deleteId, setDeleteId] = useState<string | null>(null)

  const createDialogOpen = searchParams.get('create') === 'true'
  const setCreateDialogOpen = (open: boolean) => {
    if (open) {
      setSearchParams({ create: 'true' })
    } else {
      setSearchParams({})
    }
  }

  const filtered = useMemo(() => {
    let list = sandboxes ?? []
    if (statusFilter !== 'all') {
      list = list.filter((s) => s.status === statusFilter)
    }
    if (search.trim()) {
      const q = search.toLowerCase()
      list = list.filter(
        (s) => s.id.toLowerCase().includes(q),
      )
    }
    return list.sort(
      (a, b) => new Date(b.creationTimestamp).getTime() - new Date(a.creationTimestamp).getTime(),
    )
  }, [sandboxes, statusFilter, search])

  return (
    <div className="max-w-6xl mx-auto px-6 py-8 animate-fade-in">
      {/* Header */}
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-xl font-semibold text-text-primary mb-1">Sandboxes</h1>
          <p className="text-sm text-text-secondary">
            {sandboxes?.length ?? 0} sandbox{(sandboxes?.length ?? 0) !== 1 ? 'es' : ''}
          </p>
        </div>
        <Button onClick={() => setCreateDialogOpen(true)}>
          <Plus className="w-4 h-4" />
          New Sandbox
        </Button>
      </div>

      {/* Toolbar */}
      <div className="flex items-center justify-between gap-4 mb-4">
        <div className="flex items-center gap-3 flex-1">
          {/* Search */}
          <div className="relative max-w-xs w-full">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-text-tertiary" />
            <Input
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="Search by name..."
              className="pl-9 h-8 text-xs"
            />
          </div>

          {/* Status filter chips */}
          <div className="flex items-center gap-1">
            {statusFilters.map((f) => (
              <button
                key={f.value}
                onClick={() => setStatusFilter(f.value)}
                className={cn(
                  'px-2.5 py-1 text-xs font-medium rounded-full transition-colors',
                  statusFilter === f.value
                    ? 'bg-accent/15 text-accent'
                    : 'text-text-tertiary hover:text-text-secondary hover:bg-bg-hover',
                )}
              >
                {f.label}
              </button>
            ))}
          </div>
        </div>

        {/* View toggle */}
        <div className="flex items-center gap-1 bg-bg-surface border border-border-subtle rounded-lg p-0.5">
          <button
            onClick={() => setViewMode('table')}
            className={cn(
              'p-1.5 rounded-md transition-colors',
              viewMode === 'table' ? 'bg-bg-active text-text-primary' : 'text-text-tertiary hover:text-text-secondary',
            )}
          >
            <List className="w-3.5 h-3.5" />
          </button>
          <button
            onClick={() => setViewMode('grid')}
            className={cn(
              'p-1.5 rounded-md transition-colors',
              viewMode === 'grid' ? 'bg-bg-active text-text-primary' : 'text-text-tertiary hover:text-text-secondary',
            )}
          >
            <LayoutGrid className="w-3.5 h-3.5" />
          </button>
        </div>
      </div>

      {/* Content */}
      {isLoading ? (
        viewMode === 'grid' ? (
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
            {[...Array(6)].map((_, i) => (
              <Skeleton key={i} className="h-32 rounded-xl" />
            ))}
          </div>
        ) : (
          <div className="space-y-2">
            {[...Array(5)].map((_, i) => (
              <Skeleton key={i} className="h-12 rounded-lg" />
            ))}
          </div>
        )
      ) : filtered.length === 0 ? (
        sandboxes?.length === 0 ? (
          <EmptyState
            icon={<Box className="w-6 h-6" />}
            title="No sandboxes yet"
            description="Create your first isolated environment to run code, test configurations, or experiment safely."
            action={
              <Button onClick={() => setCreateDialogOpen(true)}>
                <Plus className="w-4 h-4" />
                Create Sandbox
              </Button>
            }
          />
        ) : (
          <EmptyState
            icon={<Search className="w-6 h-6" />}
            title="No matches"
            description="No sandboxes match your current filters."
            action={
              <Button
                variant="secondary"
                onClick={() => {
                  setSearch('')
                  setStatusFilter('all')
                }}
              >
                Clear filters
              </Button>
            }
          />
        )
      ) : viewMode === 'grid' ? (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          {filtered.map((sandbox) => (
            <SandboxCard
              key={sandbox.id}
              sandbox={sandbox}
              onDelete={setDeleteId}
            />
          ))}
        </div>
      ) : (
        <div className="bg-bg-surface border border-border-subtle rounded-xl overflow-hidden">
          <SandboxTable sandboxes={filtered} onDelete={setDeleteId} />
        </div>
      )}

      {/* Dialogs */}
      <CreateSandboxDialog open={createDialogOpen} onOpenChange={setCreateDialogOpen} />
      <DeleteSandboxDialog
        sandboxId={deleteId}
        open={deleteId !== null}
        onOpenChange={(open) => !open && setDeleteId(null)}
      />
    </div>
  )
}
