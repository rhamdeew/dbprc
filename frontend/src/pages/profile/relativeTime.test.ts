import { describe, expect, it } from 'vitest';
import { relativeTime } from './relativeTime';

const now = Date.UTC(2026, 10, 11, 12, 0, 0);

describe('relativeTime', () => {
  it('formats ages from a wall timestamp', () => {
    expect(relativeTime('2026-11-11T12:00:30Z', now)).toBe('just now');
    expect(relativeTime('2026-11-11T11:59:10Z', now)).toBe('just now');
    expect(relativeTime('2026-11-11T11:59:00Z', now)).toBe('1 minute ago');
    expect(relativeTime('2026-11-11T11:58:00Z', now)).toBe('2 minutes ago');
    expect(relativeTime('2026-11-11T11:00:00Z', now)).toBe('1 hour ago');
    expect(relativeTime('2026-11-11T09:00:00Z', now)).toBe('3 hours ago');
    expect(relativeTime('2026-11-10T12:00:00Z', now)).toBe('1 day ago');
    expect(relativeTime('2026-11-08T12:00:00Z', now)).toBe('3 days ago');
    expect(relativeTime('2026-11-04T12:00:00Z', now)).toBe('Nov 4, 2026');
    expect(relativeTime('not-a-date', now)).toBe('not-a-date');
  });
});
