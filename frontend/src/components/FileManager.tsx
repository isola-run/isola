import { useState, useRef } from "react";
import { api } from "../api/client";
import type { ApiError } from "../types";

interface FileManagerProps {
  sandboxId: string;
}

export default function FileManager({ sandboxId }: FileManagerProps) {
  const [readPath, setReadPath] = useState("");
  const [readContent, setReadContent] = useState<string | null>(null);
  const [readError, setReadError] = useState<string | null>(null);
  const [reading, setReading] = useState(false);

  const [writePath, setWritePath] = useState("");
  const [writeContent, setWriteContent] = useState("");
  const [writeResult, setWriteResult] = useState<string | null>(null);
  const [writeError, setWriteError] = useState<string | null>(null);
  const [writing, setWriting] = useState(false);

  const [uploadPath, setUploadPath] = useState("");
  const [uploadResult, setUploadResult] = useState<string | null>(null);
  const [uploadError, setUploadError] = useState<string | null>(null);
  const [uploading, setUploading] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const handleRead = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!readPath.trim()) return;
    setReading(true);
    setReadContent(null);
    setReadError(null);
    try {
      const blob = await api.readFile(sandboxId, readPath.trim());
      const text = await blob.text();
      setReadContent(text);
    } catch (err: unknown) {
      const apiErr = err as ApiError;
      setReadError(apiErr.detail || apiErr.title || "Failed to read file");
    } finally {
      setReading(false);
    }
  };

  const handleWrite = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!writePath.trim()) return;
    setWriting(true);
    setWriteResult(null);
    setWriteError(null);
    try {
      const resp = await api.writeFile(sandboxId, writePath.trim(), writeContent);
      setWriteResult(
        `Written ${resp.bytesWritten} bytes to ${resp.absolutePath}`
      );
      setWriteContent("");
    } catch (err: unknown) {
      const apiErr = err as ApiError;
      setWriteError(apiErr.detail || apiErr.title || "Failed to write file");
    } finally {
      setWriting(false);
    }
  };

  const handleUpload = async (e: React.FormEvent) => {
    e.preventDefault();
    const file = fileInputRef.current?.files?.[0];
    if (!file || !uploadPath.trim()) return;
    setUploading(true);
    setUploadResult(null);
    setUploadError(null);
    try {
      const resp = await api.writeFile(
        sandboxId,
        uploadPath.trim(),
        await file.arrayBuffer()
      );
      setUploadResult(
        `Uploaded ${resp.bytesWritten} bytes to ${resp.absolutePath}`
      );
      if (fileInputRef.current) fileInputRef.current.value = "";
    } catch (err: unknown) {
      const apiErr = err as ApiError;
      setUploadError(
        apiErr.detail || apiErr.title || "Failed to upload file"
      );
    } finally {
      setUploading(false);
    }
  };

  return (
    <div className="space-y-6">
      {/* Read File */}
      <div className="rounded-lg border border-gray-800 p-4">
        <h3 className="text-sm font-medium text-gray-300 mb-3">Read File</h3>
        <form onSubmit={handleRead} className="flex gap-2">
          <input
            type="text"
            value={readPath}
            onChange={(e) => setReadPath(e.target.value)}
            className="input flex-1"
            placeholder="/path/to/file"
          />
          <button
            type="submit"
            disabled={reading || !readPath.trim()}
            className="btn-secondary"
          >
            {reading ? "Reading..." : "Read"}
          </button>
        </form>
        {readError && <p className="mt-2 text-sm text-red-400">{readError}</p>}
        {readContent !== null && (
          <pre className="mt-3 p-3 rounded bg-gray-900 border border-gray-800 text-sm text-gray-300 overflow-auto max-h-80 whitespace-pre-wrap font-mono">
            {readContent}
          </pre>
        )}
      </div>

      {/* Write File */}
      <div className="rounded-lg border border-gray-800 p-4">
        <h3 className="text-sm font-medium text-gray-300 mb-3">Write File</h3>
        <form onSubmit={handleWrite} className="space-y-2">
          <input
            type="text"
            value={writePath}
            onChange={(e) => setWritePath(e.target.value)}
            className="input w-full"
            placeholder="/path/to/file"
          />
          <textarea
            value={writeContent}
            onChange={(e) => setWriteContent(e.target.value)}
            className="input w-full font-mono"
            rows={6}
            placeholder="File content..."
          />
          <button
            type="submit"
            disabled={writing || !writePath.trim()}
            className="btn-secondary"
          >
            {writing ? "Writing..." : "Write"}
          </button>
        </form>
        {writeError && (
          <p className="mt-2 text-sm text-red-400">{writeError}</p>
        )}
        {writeResult && (
          <p className="mt-2 text-sm text-green-400">{writeResult}</p>
        )}
      </div>

      {/* Upload File */}
      <div className="rounded-lg border border-gray-800 p-4">
        <h3 className="text-sm font-medium text-gray-300 mb-3">Upload File</h3>
        <form onSubmit={handleUpload} className="space-y-2">
          <input
            type="text"
            value={uploadPath}
            onChange={(e) => setUploadPath(e.target.value)}
            className="input w-full"
            placeholder="/path/to/destination"
          />
          <input
            ref={fileInputRef}
            type="file"
            className="block text-sm text-gray-400 file:mr-3 file:py-1.5 file:px-3 file:rounded-md file:border file:border-gray-700 file:bg-gray-800 file:text-gray-300 file:text-sm file:cursor-pointer hover:file:bg-gray-700"
          />
          <button
            type="submit"
            disabled={uploading || !uploadPath.trim()}
            className="btn-secondary"
          >
            {uploading ? "Uploading..." : "Upload"}
          </button>
        </form>
        {uploadError && (
          <p className="mt-2 text-sm text-red-400">{uploadError}</p>
        )}
        {uploadResult && (
          <p className="mt-2 text-sm text-green-400">{uploadResult}</p>
        )}
      </div>
    </div>
  );
}
