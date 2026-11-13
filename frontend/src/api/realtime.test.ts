import { describe, expect, it } from 'vitest';
import { nextReconnectDelay, parseRealtimeEvent, realtimeURL, reconnectMaxMs, reconnectMinMs } from './realtime';

describe('realtime helpers', () => {
  it('waits 1s and doubles until 30s', () => {
    expect(reconnectMinMs).toBe(1000);
    expect(reconnectMaxMs).toBe(30_000);
    let delay = nextReconnectDelay(0);
    expect(delay).toBe(1000);
    const seen = [delay];
    while (delay < reconnectMaxMs) {
      delay = nextReconnectDelay(delay);
      seen.push(delay);
    }
    expect(seen).toEqual([1000, 2000, 4000, 8000, 16000, 30000]);
    expect(nextReconnectDelay(30_000)).toBe(30_000);
    expect(nextReconnectDelay(-5)).toBe(1000);
  });

  it('builds a ws url from the page', () => {
    expect(realtimeURL({ protocol: 'http:', host: 'localhost' })).toBe('ws://localhost/ws');
    expect(realtimeURL({ protocol: 'https:', host: 'arc.dbdemo.live' })).toBe('wss://arc.dbdemo.live/ws');
  });

  it('parses hub events and ignores anything else', () => {
    const chat = {
      id: 5,
      partner: { id: 3, name: 'Grace Hopper', avatarUrl: null },
      lastMessage: { body: 'hi', kind: 'text', createdAt: '2026-11-11T12:00:00Z' },
      unreadCount: 1,
    };
    const message = {
      type: 'message.new',
      data: {
        chatId: 5,
        message: { id: 8, senderId: null, kind: 'system', body: 'Matched again ☕', createdAt: '2026-11-11T12:00:00Z' },
      },
    };
    expect(parseRealtimeEvent(JSON.stringify({ type: 'chat.new', data: chat }))).toEqual({ type: 'chat.new', data: chat });
    expect(parseRealtimeEvent(message)).toEqual(message);
    expect(parseRealtimeEvent('not-json')).toBeNull();
    expect(parseRealtimeEvent('{')).toBeNull();
    expect(parseRealtimeEvent(null)).toBeNull();
    expect(parseRealtimeEvent([])).toBeNull();
    expect(parseRealtimeEvent({ type: 'other', data: {} })).toBeNull();
    expect(parseRealtimeEvent({ type: 'chat.new', data: { id: 1 } })).toBeNull();
    expect(parseRealtimeEvent({ type: 'message.new', data: { chatId: 0, message: message.data.message } })).toBeNull();
    expect(
      parseRealtimeEvent({
        type: 'message.new',
        data: { chatId: 5, message: { ...message.data.message, senderId: 'me' } },
      }),
    ).toBeNull();
  });
});
