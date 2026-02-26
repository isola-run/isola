import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogBody, DialogFooter } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { useDeleteSandbox } from '@/hooks/use-sandboxes'
import { toast } from 'sonner'
import { Loader2, AlertTriangle } from 'lucide-react'

interface DeleteSandboxDialogProps {
  sandboxId: string | null
  open: boolean
  onOpenChange: (open: boolean) => void
  onDeleted?: () => void
}

export function DeleteSandboxDialog({ sandboxId, open, onOpenChange, onDeleted }: DeleteSandboxDialogProps) {
  const deleteSandbox = useDeleteSandbox()

  const handleDelete = async () => {
    if (!sandboxId) return
    try {
      await deleteSandbox.mutateAsync(sandboxId)
      toast.success('Sandbox deleted')
      onOpenChange(false)
      onDeleted?.()
    } catch (err) {
      toast.error(`Failed to delete: ${err instanceof Error ? err.message : 'Unknown error'}`)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <div className="w-10 h-10 rounded-full bg-error/10 flex items-center justify-center mb-3">
            <AlertTriangle className="w-5 h-5 text-error" />
          </div>
          <DialogTitle>Delete Sandbox</DialogTitle>
          <DialogDescription>
            This will terminate all running commands and delete all files.
            This action cannot be undone.
          </DialogDescription>
        </DialogHeader>

        <DialogBody>
          <div className="font-mono text-sm text-text-secondary bg-bg-input rounded-lg px-3 py-2 border border-border-default">
            {sandboxId}
          </div>
        </DialogBody>

        <DialogFooter>
          <Button variant="secondary" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button variant="destructive" onClick={handleDelete} disabled={deleteSandbox.isPending}>
            {deleteSandbox.isPending ? (
              <>
                <Loader2 className="w-3.5 h-3.5 animate-spin" />
                Deleting...
              </>
            ) : (
              'Delete Sandbox'
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
