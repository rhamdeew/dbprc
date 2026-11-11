import { useQueryClient, type QueryClient } from '@tanstack/react-query';
import { useEffect, useEffectEvent } from 'react';
import {
  applyMessageToCache,
  chatsKey,
  messagesKey,
  placeChatFirst,
  type ChatMessage,
  type ChatSummary,
} from './chat';

export const reconnectMinMs = 1000;
export const reconnectMaxMs = 30_000;

/** Next wait after a dropped socket. `previous` is 0 before the first retry. */
export function nextReconnectDelay(previous: number): number {
  if (previous < reconnectMinMs) return reconnectMinMs;
  const doubled = previous * 2;
  return doubled > reconnectMaxMs ? reconnectMaxMs : doubled;
}

export function realtimeURL(location: { protocol: string; host: string }): string {
  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
  return `${protocol}//${location.host}/ws`;
}

/** Timer seam so tests can fire a reconnect without waiting out the backoff. */
export const reconnectClock = {
  delay(ms: number, fn: () => void) {
    return setTimeout(fn, ms);
  },
  cancel(id: ReturnType<typeof setTimeout>) {
    clearTimeout(id);
  },
};

type RealtimeEvent =
  | { type: 'chat.new'; data: ChatSummary }
  | { type: 'message.new'; data: { chatId: number; message: ChatMessage } };

export function parseRealtimeEvent(raw: unknown): RealtimeEvent | null {
  let value: unknown = raw;
  if (typeof raw === 'string') {
    try {
      value = JSON.parse(raw) as unknown;
    } catch {
      return null;
    }
  }
  if (!isRecord(value) || typeof value.type !== 'string') return null;
  if (value.type === 'chat.new' && isChatSummary(value.data)) {
    return { type: 'chat.new', data: value.data };
  }
  if (value.type === 'message.new' && isMessageEvent(value.data)) {
    return { type: 'message.new', data: value.data };
  }
  return null;
}

export function applyRealtime(queryClient: QueryClient, raw: unknown, openChatId: number | null) {
  const event = parseRealtimeEvent(raw);
  if (!event) return;
  if (event.type === 'chat.new') {
    const list = queryClient.getQueryData<ChatSummary[]>(chatsKey);
    if (!list) {
      void queryClient.invalidateQueries({ queryKey: chatsKey });
      return;
    }
    queryClient.setQueryData<ChatSummary[]>(chatsKey, placeChatFirst(list, event.data));
    return;
  }
  applyMessageToCache(queryClient, event.data.chatId, event.data.message, openChatId);
}

/**
 * One socket for the signed-in session. The first open does not refetch.
 * A later open invalidates the chat list and the thread that is on screen.
 * Failed connects wait 1s, then double until 30s, and reset after a successful open.
 */
export function useRealtime(openChatId: number | null) {
  const queryClient = useQueryClient();
  const openChatNow = useEffectEvent(() => openChatId);

  useEffect(() => {
    let stopped = false;
    let opened = false;
    let delay = 0;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let current: WebSocket | null = null;

    function invalidate() {
      void queryClient.invalidateQueries({ queryKey: chatsKey });
      const chatId = openChatNow();
      if (chatId !== null) void queryClient.invalidateQueries({ queryKey: messagesKey(chatId) });
    }

    function schedule() {
      if (stopped || timer !== undefined) return;
      delay = nextReconnectDelay(delay);
      const wait = delay;
      timer = reconnectClock.delay(wait, () => {
        timer = undefined;
        connect();
      });
    }

    function connect() {
      if (stopped) return;
      let socket: WebSocket;
      try {
        socket = new WebSocket(realtimeURL(window.location));
      } catch {
        schedule();
        return;
      }
      current = socket;

      socket.addEventListener('open', () => {
        if (stopped || current !== socket) return;
        if (timer !== undefined) {
          reconnectClock.cancel(timer);
          timer = undefined;
        }
        const reconnect = opened;
        opened = true;
        delay = 0;
        if (reconnect) invalidate();
      });

      socket.addEventListener('message', (event) => {
        if (stopped || current !== socket) return;
        if (typeof event.data !== 'string') return;
        applyRealtime(queryClient, event.data, openChatNow());
      });

      socket.addEventListener('close', () => {
        if (stopped || current !== socket) return;
        schedule();
      });

      socket.addEventListener('error', () => {
        if (stopped || current !== socket) return;
        if (socket.readyState !== WebSocket.CLOSING && socket.readyState !== WebSocket.CLOSED) {
          try {
            socket.close();
          } catch {
            schedule();
          }
        }
      });
    }

    connect();

    return () => {
      stopped = true;
      if (timer !== undefined) reconnectClock.cancel(timer);
      if (current && current.readyState !== WebSocket.CLOSED) current.close();
    };
  }, [queryClient]);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function isId(value: unknown): value is number {
  return typeof value === 'number' && Number.isSafeInteger(value) && value > 0;
}

function isChatSummary(value: unknown): value is ChatSummary {
  if (!isRecord(value) || !isId(value.id) || !isRecord(value.partner)) return false;
  const partner = value.partner;
  if (!isId(partner.id) || typeof partner.name !== 'string') return false;
  if (partner.avatarUrl !== null && typeof partner.avatarUrl !== 'string') return false;
  if (typeof value.unreadCount !== 'number') return false;
  if (value.lastMessage === null) return true;
  return isLastMessage(value.lastMessage);
}

function isLastMessage(value: unknown): value is ChatSummary['lastMessage'] {
  if (!isRecord(value)) return false;
  return typeof value.body === 'string' && typeof value.kind === 'string' && typeof value.createdAt === 'string';
}

function isMessage(value: unknown): value is ChatMessage {
  if (!isRecord(value) || !isId(value.id)) return false;
  if (value.senderId !== null && !isId(value.senderId)) return false;
  return typeof value.kind === 'string' && typeof value.body === 'string' && typeof value.createdAt === 'string';
}

function isMessageEvent(value: unknown): value is { chatId: number; message: ChatMessage } {
  if (!isRecord(value) || !isId(value.chatId)) return false;
  return isMessage(value.message);
}
