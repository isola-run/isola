import { useState, useRef } from 'react';
import {
  Upload,
  File,
  FolderOpen,
  AlertCircle,
  Check,
  X,
  Loader2,
} from 'lucide-react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { useUploadFile } from '@/hooks/useSandboxes';
import { cn } from '@/lib/utils';

interface SandboxFilesProps {
  sandboxId: string;
}

interface UploadedFile {
  id: string;
  name: string;
  path: string;
  size: number;
  status: 'success' | 'error';
  error?: string;
}

function SandboxFiles({ sandboxId }: SandboxFilesProps) {
  const [targetPath, setTargetPath] = useState('/workspace');
  const [isDragging, setIsDragging] = useState(false);
  const [uploadedFiles, setUploadedFiles] = useState<UploadedFile[]>([]);
  const [isUploading, setIsUploading] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const uploadMutation = useUploadFile(sandboxId);

  const handleDragOver = (e: React.DragEvent) => {
    e.preventDefault();
    setIsDragging(true);
  };

  const handleDragLeave = (e: React.DragEvent) => {
    e.preventDefault();
    setIsDragging(false);
  };

  const handleDrop = async (e: React.DragEvent) => {
    e.preventDefault();
    setIsDragging(false);

    const files = Array.from(e.dataTransfer.files);
    await uploadFiles(files);
  };

  const handleFileSelect = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const files = Array.from(e.target.files || []);
    await uploadFiles(files);
    if (fileInputRef.current) {
      fileInputRef.current.value = '';
    }
  };

  const uploadFiles = async (files: File[]) => {
    if (files.length === 0) return;

    setIsUploading(true);

    for (const file of files) {
      const filePath = targetPath.endsWith('/')
        ? `${targetPath}${file.name}`
        : `${targetPath}/${file.name}`;

      try {
        const result = await uploadMutation.mutateAsync({
          file,
          path: filePath,
        });

        setUploadedFiles((prev) => [
          {
            id: Date.now().toString() + file.name,
            name: file.name,
            path: result.path,
            size: result.size,
            status: 'success',
          },
          ...prev,
        ]);
      } catch (error) {
        setUploadedFiles((prev) => [
          {
            id: Date.now().toString() + file.name,
            name: file.name,
            path: filePath,
            size: file.size,
            status: 'error',
            error: (error as Error).message,
          },
          ...prev,
        ]);
      }
    }

    setIsUploading(false);
  };

  const formatSize = (bytes: number): string => {
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  };

  const removeUploadedFile = (id: string) => {
    setUploadedFiles((prev) => prev.filter((f) => f.id !== id));
  };

  return (
    <div className="space-y-6">
      {/* Upload Section */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Upload className="h-5 w-5" />
            Upload Files
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          {/* Target path input */}
          <Input
            label="Target Directory"
            value={targetPath}
            onChange={(e) => setTargetPath(e.target.value)}
            placeholder="/workspace"
            hint="Files will be uploaded to this directory in the sandbox"
          />

          {/* Drop zone */}
          <div
            onDragOver={handleDragOver}
            onDragLeave={handleDragLeave}
            onDrop={handleDrop}
            className={cn(
              'relative border-2 border-dashed rounded-lg p-8 text-center transition-colors cursor-pointer',
              isDragging
                ? 'border-primary bg-primary/5'
                : 'border-muted-foreground/25 hover:border-muted-foreground/50'
            )}
            onClick={() => fileInputRef.current?.click()}
          >
            <input
              ref={fileInputRef}
              type="file"
              multiple
              className="hidden"
              onChange={handleFileSelect}
            />

            {isUploading ? (
              <div className="flex flex-col items-center gap-3">
                <Loader2 className="h-10 w-10 text-primary animate-spin" />
                <p className="text-sm text-muted-foreground">Uploading files...</p>
              </div>
            ) : (
              <div className="flex flex-col items-center gap-3">
                <div className="flex h-14 w-14 items-center justify-center rounded-full bg-muted">
                  <Upload className="h-6 w-6 text-muted-foreground" />
                </div>
                <div>
                  <p className="font-medium">Drop files here or click to upload</p>
                  <p className="text-sm text-muted-foreground mt-1">
                    Maximum file size: 5MB per file
                  </p>
                </div>
              </div>
            )}
          </div>
        </CardContent>
      </Card>

      {/* Uploaded Files List */}
      {uploadedFiles.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center justify-between">
              <span className="flex items-center gap-2">
                <FolderOpen className="h-5 w-5" />
                Uploaded Files
              </span>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => setUploadedFiles([])}
              >
                Clear All
              </Button>
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="space-y-2">
              {uploadedFiles.map((file) => (
                <div
                  key={file.id}
                  className={cn(
                    'flex items-center gap-3 p-3 rounded-lg border',
                    file.status === 'success'
                      ? 'bg-emerald-50 dark:bg-emerald-900/10 border-emerald-200 dark:border-emerald-900/30'
                      : 'bg-destructive/10 border-destructive/20'
                  )}
                >
                  <div
                    className={cn(
                      'flex h-9 w-9 items-center justify-center rounded-lg',
                      file.status === 'success'
                        ? 'bg-emerald-100 dark:bg-emerald-900/30'
                        : 'bg-destructive/20'
                    )}
                  >
                    {file.status === 'success' ? (
                      <Check className="h-4 w-4 text-emerald-600 dark:text-emerald-400" />
                    ) : (
                      <AlertCircle className="h-4 w-4 text-destructive" />
                    )}
                  </div>

                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <File className="h-4 w-4 text-muted-foreground flex-shrink-0" />
                      <span className="font-medium truncate">{file.name}</span>
                    </div>
                    <div className="flex items-center gap-2 mt-0.5 text-xs text-muted-foreground">
                      <span className="font-mono truncate">{file.path}</span>
                      <span>•</span>
                      <span>{formatSize(file.size)}</span>
                    </div>
                    {file.error && (
                      <p className="text-xs text-destructive mt-1">{file.error}</p>
                    )}
                  </div>

                  <Button
                    variant="ghost"
                    size="icon"
                    className="h-8 w-8 flex-shrink-0"
                    onClick={() => removeUploadedFile(file.id)}
                  >
                    <X className="h-4 w-4" />
                  </Button>
                </div>
              ))}
            </div>
          </CardContent>
        </Card>
      )}

      {/* Help text */}
      <Card>
        <CardContent className="pt-6">
          <div className="flex items-start gap-3">
            <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-muted flex-shrink-0">
              <AlertCircle className="h-4 w-4 text-muted-foreground" />
            </div>
            <div className="text-sm text-muted-foreground">
              <p className="font-medium text-foreground">Tips for file uploads</p>
              <ul className="mt-2 list-disc list-inside space-y-1">
                <li>Files larger than 5MB require a different upload method</li>
                <li>Ensure the target directory exists in the sandbox</li>
                <li>You can use the terminal to verify uploaded files</li>
                <li>Multiple files can be uploaded at once via drag and drop</li>
              </ul>
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}

export { SandboxFiles };
