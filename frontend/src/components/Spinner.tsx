export default function Spinner({ label = "Loading..." }: { label?: string }) {
  return (
    <div className="text-center py-20 text-gray-500" role="status" aria-label={label}>
      <div className="inline-block w-6 h-6 border-2 border-gray-600 border-t-indigo-500 rounded-full animate-spin" />
      <p className="mt-3">{label}</p>
    </div>
  );
}
