import { describe, expect, it } from 'vitest';
import { clockTime, dayLabel } from './time';

// Local-time constructors keep these independent of the machine's time zone.
const now = new Date(2026, 10, 11, 15, 30);

describe('message times', () => {
  it('shows a 24-hour clock', () => {
    expect(clockTime(new Date(2026, 10, 11, 9, 5).toISOString())).toBe('09:05');
    expect(clockTime(new Date(2026, 10, 11, 14, 5).toISOString())).toBe('14:05');
  });

  it('returns an unreadable timestamp unchanged', () => {
    expect(clockTime('soon')).toBe('soon');
    expect(dayLabel('soon', now)).toBe('');
  });

  it('labels today, yesterday, and older days', () => {
    expect(dayLabel(new Date(2026, 10, 11, 0, 1).toISOString(), now)).toBe('Today');
    expect(dayLabel(new Date(2026, 10, 12, 8, 0).toISOString(), now)).toBe('Today');
    expect(dayLabel(new Date(2026, 10, 10, 23, 59).toISOString(), now)).toBe('Yesterday');
    expect(dayLabel(new Date(2026, 2, 3, 12, 0).toISOString(), now)).toBe('Mar 3');
    expect(dayLabel(new Date(2025, 2, 3, 12, 0).toISOString(), now)).toBe('Mar 3, 2025');
  });
});
