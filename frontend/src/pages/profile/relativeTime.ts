const minute = 60_000;
const hour = 60 * minute;
const day = 24 * hour;
const week = 7 * day;

/** Short English age for a wall timestamp. Older than a week is a UTC calendar date. */
export function relativeTime(iso: string, now = Date.now()): string {
  const then = Date.parse(iso);
  if (Number.isNaN(then)) return iso;
  const delta = now - then;
  if (delta < minute) return 'just now';
  if (delta < hour) return ago(Math.floor(delta / minute), 'minute');
  if (delta < day) return ago(Math.floor(delta / hour), 'hour');
  if (delta < week) return ago(Math.floor(delta / day), 'day');
  return new Intl.DateTimeFormat('en-US', {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
    timeZone: 'UTC',
  }).format(new Date(then));
}

function ago(count: number, unit: string): string {
  return count === 1 ? `1 ${unit} ago` : `${count} ${unit}s ago`;
}
