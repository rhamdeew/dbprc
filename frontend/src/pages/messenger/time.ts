/** Clock time of a message in the viewer's time zone: "14:05". An unreadable timestamp is returned as-is. */
export function clockTime(iso: string): string {
  const then = new Date(iso);
  if (Number.isNaN(then.getTime())) return iso;
  return then.toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit' });
}

function dayStart(date: Date): number {
  return new Date(date.getFullYear(), date.getMonth(), date.getDate()).getTime();
}

/** Day separator in the viewer's time zone: "Today", "Yesterday", "Mar 3", "Mar 3, 2025". */
export function dayLabel(iso: string, now: Date = new Date()): string {
  const then = new Date(iso);
  if (Number.isNaN(then.getTime())) return '';
  const days = Math.round((dayStart(now) - dayStart(then)) / 86_400_000);
  if (days <= 0) return 'Today';
  if (days === 1) return 'Yesterday';
  return then.toLocaleDateString('en-US', {
    month: 'short',
    day: 'numeric',
    year: then.getFullYear() === now.getFullYear() ? undefined : 'numeric',
  });
}
