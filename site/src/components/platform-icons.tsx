import { Command } from "lucide-react";

export function WindowsIcon({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" aria-hidden="true" className={className} fill="currentColor">
      <path d="M3 5.4 10.5 4.4v7.1H3zM11.5 4.25 21 3v8.5h-9.5zM3 12.5h7.5v7.1L3 18.6zM11.5 12.5H21V21l-9.5-1.25z" />
    </svg>
  );
}

export function MacIcon({ className }: { className?: string }) {
  return <Command aria-hidden="true" className={className} />;
}
