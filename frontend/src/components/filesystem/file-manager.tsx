import { useState, useCallback, useRef } from 'react'
import { Upload, Download, FolderUp, FileIcon, CheckCircle2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input, Label } from '@/components/ui/input'
import { useUploadFile, useDownloadFile } from '@/hooks/use-filesystem'
import { cn } from '@/lib/utils'
import { toast } from 'sonner'

interface FileManagerProps {
  sandboxId: string
  disabled?: boolean
}

interface FileTransfer {
  id: string
  name: string
  type: 'upload' | 'download'
  status: 'pending' | 'complete' | 'error'
  path: string
  size?: number
}

export function FileManager({ sandboxId, disabled }: FileManagerProps) {
  const [transfers, setTransfers] = useState<FileTransfer[]>([])
  const [downloadPath, setDownloadPath] = useState('')
  const [uploadPath, setUploadPath] = useState('/tmp/')
  const [isDragging, setIsDragging] = useState(false)
  const fileInputRef = useRef<HTMLInputElement>(null)

  const uploadFile = useUploadFile(sandboxId)
  const downloadFile = useDownloadFile(sandboxId)

  const handleUpload = useCallback(async (files: FileList | File[]) => {
    for (const file of Array.from(files)) {
      const path = uploadPath.endsWith('/') ? `${uploadPath}${file.name}` : uploadPath
      const transferId = crypto.randomUUID()

      setTransfers((prev) => [
        { id: transferId, name: file.name, type: 'upload', status: 'pending', path, size: file.size },
        ...prev,
      ])

      try {
        await uploadFile.mutateAsync({ path, file })
        setTransfers((prev) =>
          prev.map((t) => (t.id === transferId ? { ...t, status: 'complete' as const } : t)),
        )
        toast.success(`Uploaded ${file.name}`)
      } catch (err) {
        setTransfers((prev) =>
          prev.map((t) => (t.id === transferId ? { ...t, status: 'error' as const } : t)),
        )
        toast.error(`Upload failed: ${err instanceof Error ? err.message : 'Unknown error'}`)
      }
    }
  }, [uploadPath, uploadFile])

  const handleDownload = useCallback(async () => {
    if (!downloadPath.trim()) return
    try {
      await downloadFile.mutateAsync(downloadPath)
      toast.success(`Downloaded ${downloadPath.split('/').pop()}`)
    } catch (err) {
      toast.error(`Download failed: ${err instanceof Error ? err.message : 'Unknown error'}`)
    }
  }, [downloadPath, downloadFile])

  const handleDragOver = (e: React.DragEvent) => {
    e.preventDefault()
    setIsDragging(true)
  }

  const handleDragLeave = () => setIsDragging(false)

  const handleDrop = (e: React.DragEvent) => {
    e.preventDefault()
    setIsDragging(false)
    if (e.dataTransfer.files.length > 0) {
      handleUpload(e.dataTransfer.files)
    }
  }

  return (
    <div className="flex flex-col h-full p-4 space-y-4">
      {/* Upload section */}
      <div className="space-y-3">
        <h3 className="text-sm font-semibold text-text-primary flex items-center gap-2">
          <Upload className="w-4 h-4 text-accent" />
          Upload Files
        </h3>

        <div className="space-y-2">
          <div className="space-y-1.5">
            <Label>Destination Path</Label>
            <Input
              value={uploadPath}
              onChange={(e) => setUploadPath(e.target.value)}
              placeholder="/tmp/myfile.txt"
              className="font-mono text-xs"
            />
          </div>
        </div>

        {/* Drop zone */}
        <div
          onDragOver={handleDragOver}
          onDragLeave={handleDragLeave}
          onDrop={handleDrop}
          onClick={() => !disabled && fileInputRef.current?.click()}
          className={cn(
            'relative rounded-lg border-2 border-dashed p-6 text-center transition-all duration-200 cursor-pointer',
            isDragging
              ? 'border-accent bg-accent-muted'
              : 'border-border-default hover:border-border-emphasis hover:bg-bg-hover/30',
            disabled && 'opacity-50 pointer-events-none',
          )}
        >
          <FolderUp className="w-8 h-8 text-text-tertiary mx-auto mb-2" />
          <p className="text-sm text-text-secondary">
            {isDragging ? 'Drop files here' : 'Drag and drop files or click to browse'}
          </p>
          <input
            ref={fileInputRef}
            type="file"
            multiple
            className="hidden"
            onChange={(e) => e.target.files && handleUpload(e.target.files)}
          />
        </div>
      </div>

      {/* Download section */}
      <div className="space-y-3">
        <h3 className="text-sm font-semibold text-text-primary flex items-center gap-2">
          <Download className="w-4 h-4 text-accent" />
          Download File
        </h3>

        <div className="flex items-end gap-2">
          <div className="flex-1 space-y-1.5">
            <Label>File Path</Label>
            <Input
              value={downloadPath}
              onChange={(e) => setDownloadPath(e.target.value)}
              placeholder="/etc/hostname"
              className="font-mono text-xs"
              onKeyDown={(e) => e.key === 'Enter' && handleDownload()}
            />
          </div>
          <Button
            variant="secondary"
            size="md"
            onClick={handleDownload}
            disabled={!downloadPath.trim() || downloadFile.isPending || disabled}
          >
            <Download className="w-3.5 h-3.5" />
            Download
          </Button>
        </div>
      </div>

      {/* Transfer history */}
      {transfers.length > 0 && (
        <div className="space-y-2">
          <h3 className="text-sm font-semibold text-text-primary">Recent Transfers</h3>
          <div className="space-y-1">
            {transfers.slice(0, 10).map((transfer) => (
              <div
                key={transfer.id}
                className="flex items-center gap-2 px-3 py-2 rounded-lg bg-bg-surface border border-border-subtle"
              >
                {transfer.status === 'complete' ? (
                  <CheckCircle2 className="w-3.5 h-3.5 text-success shrink-0" />
                ) : transfer.status === 'error' ? (
                  <span className="w-3.5 h-3.5 rounded-full bg-error/20 shrink-0" />
                ) : (
                  <span className="w-3.5 h-3.5 rounded-full bg-accent/20 animate-pulse shrink-0" />
                )}
                <FileIcon className="w-3.5 h-3.5 text-text-tertiary shrink-0" />
                <span className="text-xs font-mono text-text-secondary truncate flex-1">{transfer.path}</span>
                <span className="text-[11px] text-text-tertiary capitalize">{transfer.type}</span>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
