const statusStyles: Record<string, string> = {
  creating: "bg-yellow-900/50 text-yellow-300 border-yellow-700",
  running: "bg-green-900/50 text-green-300 border-green-700",
  shuttingDown: "bg-orange-900/50 text-orange-300 border-orange-700",
  failed: "bg-red-900/50 text-red-300 border-red-700",
  stopped: "bg-gray-800 text-gray-400 border-gray-600",
  unknown: "bg-gray-800 text-gray-500 border-gray-600",
  pending: "bg-yellow-900/50 text-yellow-300 border-yellow-700",
  inProgress: "bg-blue-900/50 text-blue-300 border-blue-700",
  complete: "bg-green-900/50 text-green-300 border-green-700",
};

const statusDots: Record<string, string> = {
  creating: "bg-yellow-400",
  running: "bg-green-400",
  shuttingDown: "bg-orange-400",
  failed: "bg-red-400",
  stopped: "bg-gray-500",
  unknown: "bg-gray-500",
  pending: "bg-yellow-400",
  inProgress: "bg-blue-400 animate-pulse",
  complete: "bg-green-400",
};

export default function StatusBadge({ status }: { status: string }) {
  const style = statusStyles[status] ?? statusStyles.unknown;
  const dot = statusDots[status] ?? statusDots.unknown;

  return (
    <span
      className={`inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-medium border ${style}`}
    >
      <span className={`w-1.5 h-1.5 rounded-full ${dot}`} />
      {status}
    </span>
  );
}
