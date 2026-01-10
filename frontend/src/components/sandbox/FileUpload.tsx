import { useState, useRef } from 'react'
import { clsx } from 'clsx'
import { Upload, File, X, Check, AlertCircle } from 'lucide-react'
import { Button, Input } from '@/components/ui'
import { apiClient } from '@/api/client'

interface FileUploadProps {
  sandboxId: string
  disabled?: boolean
  onUploadComplete?: () => void
}

interface UploadFile {
  file: File
  path: string
  status: 'pending' | 'uploading' | 'success' | 'error'
  error?: string
}

export function FileUpload({
  sandboxId,
  disabled = false,
  onUploadComplete,
}: FileUploadProps) {
  const [files, setFiles] = useState<UploadFile[]>([])
  const [defaultPath, setDefaultPath] = useState('/tmp/')
  const [isDragging, setIsDragging] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)

  const handleFileSelect = (selectedFiles: FileList | null) => {
    if (!selectedFiles) return

    const newFiles: UploadFile[] = Array.from(selectedFiles).map((file) => ({
      file,
      path: `${defaultPath}${file.name}`,
      status: 'pending',
    }))

    setFiles((prev) => [...prev, ...newFiles])
  }

  const handleDrop = (e: React.DragEvent) => {
    e.preventDefault()
    setIsDragging(false)
    handleFileSelect(e.dataTransfer.files)
  }

  const handleDragOver = (e: React.DragEvent) => {
    e.preventDefault()
    setIsDragging(true)
  }

  const handleDragLeave = () => {
    setIsDragging(false)
  }

  const removeFile = (index: number) => {
    setFiles((prev) => prev.filter((_, i) => i !== index))
  }

  const updateFilePath = (index: number, path: string) => {
    setFiles((prev) =>
      prev.map((f, i) => (i === index ? { ...f, path } : f))
    )
  }

  const uploadFile = async (index: number) => {
    const uploadFile = files[index]
    if (!uploadFile || uploadFile.status !== 'pending') return

    setFiles((prev) =>
      prev.map((f, i) =>
        i === index ? { ...f, status: 'uploading' as const } : f
      )
    )

    try {
      await apiClient.uploadFile(sandboxId, uploadFile.file, uploadFile.path)
      setFiles((prev) =>
        prev.map((f, i) =>
          i === index ? { ...f, status: 'success' as const } : f
        )
      )
      onUploadComplete?.()
    } catch (err) {
      setFiles((prev) =>
        prev.map((f, i) =>
          i === index
            ? { ...f, status: 'error' as const, error: (err as Error).message }
            : f
        )
      )
    }
  }

  const uploadAll = async () => {
    const pendingFiles = files
      .map((_, i) => i)
      .filter((i) => files[i].status === 'pending')

    for (const index of pendingFiles) {
      await uploadFile(index)
    }
  }

  const pendingCount = files.filter((f) => f.status === 'pending').length
  const hasFiles = files.length > 0

  return (
    <div className="space-y-4">
      {/* Default path */}
      <Input
        label="Default upload path"
        value={defaultPath}
        onChange={(e) => setDefaultPath(e.target.value)}
        placeholder="/tmp/"
        disabled={disabled}
      />

      {/* Drop zone */}
      <div
        onDrop={handleDrop}
        onDragOver={handleDragOver}
        onDragLeave={handleDragLeave}
        onClick={() => inputRef.current?.click()}
        className={clsx(
          'border-2 border-dashed rounded-lg p-8 text-center cursor-pointer transition-colors',
          isDragging
            ? 'border-primary-400 bg-primary-50'
            : 'border-slate-300 hover:border-slate-400',
          disabled && 'opacity-50 cursor-not-allowed'
        )}
      >
        <input
          ref={inputRef}
          type="file"
          multiple
          onChange={(e) => handleFileSelect(e.target.files)}
          className="hidden"
          disabled={disabled}
        />
        <Upload className="h-8 w-8 mx-auto mb-2 text-slate-400" />
        <p className="text-sm text-slate-600">
          Drop files here or <span className="text-primary-600">browse</span>
        </p>
        <p className="text-xs text-slate-400 mt-1">Max file size: 5MB</p>
      </div>

      {/* File list */}
      {hasFiles && (
        <div className="space-y-2">
          {files.map((uploadFile, index) => (
            <div
              key={index}
              className="flex items-center gap-3 p-3 bg-slate-50 rounded-lg"
            >
              <File className="h-5 w-5 text-slate-400 flex-shrink-0" />
              <div className="flex-1 min-w-0">
                <p className="text-sm font-medium text-slate-900 truncate">
                  {uploadFile.file.name}
                </p>
                {uploadFile.status === 'pending' ? (
                  <input
                    type="text"
                    value={uploadFile.path}
                    onChange={(e) => updateFilePath(index, e.target.value)}
                    className="w-full text-xs text-slate-500 bg-transparent border-b border-slate-200 focus:border-primary-400 outline-none mt-1"
                    placeholder="Destination path"
                  />
                ) : (
                  <p className="text-xs text-slate-500 truncate">
                    {uploadFile.path}
                  </p>
                )}
                {uploadFile.error && (
                  <p className="text-xs text-red-600 mt-1">{uploadFile.error}</p>
                )}
              </div>
              <div className="flex-shrink-0">
                {uploadFile.status === 'pending' && (
                  <button
                    onClick={() => removeFile(index)}
                    className="p-1 text-slate-400 hover:text-slate-600"
                  >
                    <X className="h-4 w-4" />
                  </button>
                )}
                {uploadFile.status === 'uploading' && (
                  <div className="h-5 w-5 border-2 border-primary-500 border-t-transparent rounded-full animate-spin" />
                )}
                {uploadFile.status === 'success' && (
                  <Check className="h-5 w-5 text-emerald-500" />
                )}
                {uploadFile.status === 'error' && (
                  <AlertCircle className="h-5 w-5 text-red-500" />
                )}
              </div>
            </div>
          ))}

          {pendingCount > 0 && (
            <Button onClick={uploadAll} disabled={disabled} className="w-full">
              Upload {pendingCount} file{pendingCount !== 1 ? 's' : ''}
            </Button>
          )}
        </div>
      )}
    </div>
  )
}
