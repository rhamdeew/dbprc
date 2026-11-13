import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
  type InfiniteData,
  type QueryClient,
} from '@tanstack/react-query';
import { useEffect, useRef, useState } from 'react';
import { api } from './client';
import { meKey, type Me } from './me';

/** One row of `GET /api/chats` (ARC-25). `chat.new` uses the same shape. */
export type ChatPartner = {
  id: number;
  name: string;
  avatarUrl: string | null;
};

export type ChatLastMessage = {
  body: string;
  kind: string;
  createdAt: string;
};

export type ChatSummary = {
  id: number;
  partner: ChatPartner;
  lastMessage: ChatLastMessage | null;
  unreadCount: number;
};

/** One row of `GET /api/chats/{id}/messages`. `senderId` is null for a system line. */
export type ChatMessage = {
  id: number;
  senderId: number | null;
  kind: string;
  body: string;
  createdAt: string;
};

export type RandomChat = {
  chat: ChatSummary;
  rematch: boolean;
};

export const chatsKey = ['chats'] as const;

export function messagesKey(chatId: number) {
  return ['chat-messages', chatId] as const;
}

/** Page size sent as `limit`. A shorter page means there is nothing older. */
export const messagePageSize = 50;

type MessagePage = {
  messages: ChatMessage[];
  hasMore: boolean;
};

export type MessageCache = InfiniteData<MessagePage, number>;

export function parseChatId(raw: string | undefined): number | null {
  if (raw === undefined || !/^[1-9]\d*$/.test(raw)) return null;
  const id = Number(raw);
  return Number.isSafeInteger(id) ? id : null;
}

export function useChats() {
  return useQuery({
    queryKey: chatsKey,
    queryFn: () => api<ChatSummary[]>('/api/chats'),
    retry: false,
  });
}

export function useMessages(chatId: number | null) {
  return useInfiniteQuery<MessagePage, Error, MessageCache, ReturnType<typeof messagesKey>, number>({
    queryKey: messagesKey(chatId ?? 0),
    queryFn: ({ pageParam }) => fetchMessages(chatId ?? 0, pageParam),
    initialPageParam: 0,
    getNextPageParam: (last) => {
      if (!last.hasMore) return undefined;
      return last.messages[0]?.id;
    },
    enabled: chatId !== null,
    retry: false,
  });
}

async function fetchMessages(chatId: number, before: number): Promise<MessagePage> {
  const params = new URLSearchParams({ limit: String(messagePageSize) });
  if (before > 0) params.set('before', String(before));
  const page = await api<{ messages: ChatMessage[] }>(`/api/chats/${chatId}/messages?${params.toString()}`);
  const messages = Array.isArray(page.messages) ? page.messages : [];
  return { messages, hasMore: messages.length === messagePageSize };
}

/** Oldest page first. Page 0 in the cache is the latest window. */
export function flattenMessages(data: MessageCache | undefined): ChatMessage[] {
  if (!data) return [];
  return [...data.pages].reverse().flatMap((page) => page.messages);
}

export function latestServerMessageId(messages: ChatMessage[]): number | null {
  for (let index = messages.length - 1; index >= 0; index -= 1) {
    const id = messages[index]?.id;
    if (id !== undefined && id > 0) return id;
  }
  return null;
}

export function useSetAvailability() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (available: boolean) =>
      api<{ available: boolean }>('/api/me/availability', { method: 'PUT', body: { available } }),
    onMutate: async (available) => {
      await queryClient.cancelQueries({ queryKey: meKey });
      const previous = queryClient.getQueryData<Me>(meKey);
      queryClient.setQueryData<Me>(meKey, (me) => (me ? { ...me, available } : me));
      return { previous };
    },
    onError: (_error, _available, context) => {
      if (context?.previous) queryClient.setQueryData(meKey, context.previous);
    },
    onSuccess: (data) => {
      queryClient.setQueryData<Me>(meKey, (me) => (me ? { ...me, available: data.available } : me));
    },
  });
}

export function useStartRandomChat() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => api<RandomChat>('/api/chats/random', { method: 'POST', silent: true }),
    onSuccess: (result) => {
      queryClient.setQueryData<ChatSummary[]>(chatsKey, (list) => placeChatFirst(list ?? [], result.chat));
    },
  });
}

export function useSendMessage(chatId: number) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: string) =>
      api<ChatMessage>(`/api/chats/${chatId}/messages`, { method: 'POST', body: { body } }),
    onMutate: async (body) => {
      await queryClient.cancelQueries({ queryKey: messagesKey(chatId) });
      const me = queryClient.getQueryData<Me>(meKey);
      const optimistic: ChatMessage = {
        id: nextTempId(),
        senderId: me?.id ?? null,
        kind: 'text',
        body,
        createdAt: new Date().toISOString(),
      };
      const previousMessages = queryClient.getQueryData<MessageCache>(messagesKey(chatId));
      const previousChats = queryClient.getQueryData<ChatSummary[]>(chatsKey);
      queryClient.setQueryData<MessageCache>(messagesKey(chatId), (current) => appendOptimistic(current, optimistic));
      queryClient.setQueryData<ChatSummary[]>(chatsKey, (list) =>
        noteIncoming(list, chatId, optimistic, { mine: true, visible: !document.hidden }),
      );
      return { tempId: optimistic.id, previousMessages, previousChats };
    },
    onSuccess: (saved, _body, context) => {
      queryClient.setQueryData<MessageCache>(messagesKey(chatId), (current) =>
        settleOptimistic(current, context.tempId, saved),
      );
      queryClient.setQueryData<ChatSummary[]>(chatsKey, (list) =>
        noteIncoming(list, chatId, saved, { mine: true, visible: !document.hidden }),
      );
    },
    onError: (_error, _body, context) => {
      if (!context) return;
      queryClient.setQueryData(messagesKey(chatId), context.previousMessages);
      queryClient.setQueryData(chatsKey, context.previousChats);
    },
  });
}

/** Clears the badge while this chat is on screen and advances the read cursor. */
export function useMarkThreadRead(chatId: number, lastMessageId: number | null) {
  const queryClient = useQueryClient();
  const chats = useChats();
  const sent = useRef<{ chatId: number; messageId: number } | null>(null);
  const [visible, setVisible] = useState(() => !document.hidden);

  useEffect(() => {
    const onVisibilityChange = () => setVisible(!document.hidden);
    document.addEventListener('visibilitychange', onVisibilityChange);
    onVisibilityChange();
    return () => document.removeEventListener('visibilitychange', onVisibilityChange);
  }, []);

  useEffect(() => {
    if (!visible || document.hidden || lastMessageId === null) return;
    const list = chats.data;
    if (!list?.some((chat) => chat.id === chatId && chat.unreadCount > 0)) return;
    queryClient.setQueryData<ChatSummary[]>(chatsKey, zeroUnread(list, chatId));
  }, [chatId, chats.data, lastMessageId, queryClient, visible]);

  useEffect(() => {
    if (!visible || document.hidden || lastMessageId === null || lastMessageId <= 0) return;
    if (sent.current?.chatId === chatId && sent.current.messageId === lastMessageId) return;
    sent.current = { chatId, messageId: lastMessageId };
    let cancelled = false;
    api<{ lastMessageId: number }>(`/api/chats/${chatId}/read`, {
      method: 'POST',
      body: { lastMessageId },
      silent: true,
    })
      .then(() => {
        if (cancelled || document.hidden) return;
        queryClient.setQueryData<ChatSummary[]>(chatsKey, (list) => zeroUnread(list, chatId));
      })
      .catch(() => {
        // The badge is already cleared locally. A later message retries with a new id.
      });
    return () => {
      cancelled = true;
    };
  }, [chatId, lastMessageId, queryClient, visible]);
}

let tempId = 0;

function nextTempId(): number {
  tempId -= 1;
  return tempId;
}

export function placeChatFirst(list: ChatSummary[], chat: ChatSummary): ChatSummary[] {
  return [chat, ...list.filter((item) => item.id !== chat.id)];
}

export function noteIncoming(
  list: ChatSummary[] | undefined,
  chatId: number,
  message: ChatMessage,
  options: { mine: boolean; visible: boolean },
): ChatSummary[] | undefined {
  if (!list) return list;
  const current = list.find((item) => item.id === chatId);
  if (!current) return list;
  let unreadCount = current.unreadCount;
  if (options.visible) unreadCount = 0;
  else if (!options.mine) unreadCount += 1;
  const next: ChatSummary = {
    ...current,
    lastMessage: { body: message.body, kind: message.kind, createdAt: message.createdAt },
    unreadCount,
  };
  return placeChatFirst(list, next);
}

export function zeroUnread(list: ChatSummary[] | undefined, chatId: number): ChatSummary[] | undefined {
  if (!list) return list;
  if (!list.some((chat) => chat.id === chatId && chat.unreadCount > 0)) return list;
  return list.map((chat) => (chat.id === chatId ? { ...chat, unreadCount: 0 } : chat));
}

function mapLatest(current: MessageCache | undefined, update: (messages: ChatMessage[]) => ChatMessage[]): MessageCache {
  if (!current || current.pages.length === 0) {
    return {
      pages: [{ messages: update([]), hasMore: false }],
      pageParams: [0],
    };
  }
  return {
    ...current,
    pages: current.pages.map((page, index) => (index === 0 ? { ...page, messages: update(page.messages) } : page)),
  };
}

export function appendOptimistic(current: MessageCache | undefined, message: ChatMessage): MessageCache {
  return mapLatest(current, (messages) => [...messages, message]);
}

export function settleOptimistic(current: MessageCache | undefined, temp: number, saved: ChatMessage): MessageCache | undefined {
  if (!current) return current;
  return mapLatest(current, (messages) => {
    const withoutTemp = messages.filter((item) => item.id !== temp);
    if (withoutTemp.some((item) => item.id === saved.id)) return withoutTemp;
    const index = messages.findIndex((item) => item.id === temp);
    if (index < 0) return [...withoutTemp, saved];
    const next = messages.slice();
    next[index] = saved;
    return next;
  });
}

export function ingestMessage(current: MessageCache | undefined, message: ChatMessage): MessageCache | undefined {
  if (!current) return current;
  if (current.pages.some((page) => page.messages.some((item) => item.id === message.id))) return current;
  return mapLatest(current, (messages) => {
    const optimistic = messages.findIndex(
      (item) => item.id < 0 && item.senderId === message.senderId && item.body === message.body,
    );
    if (optimistic < 0) return [...messages, message];
    const next = messages.slice();
    next[optimistic] = message;
    return next;
  });
}

export function applyMessageToCache(queryClient: QueryClient, chatId: number, message: ChatMessage, openChatId: number | null) {
  const me = queryClient.getQueryData<Me>(meKey);
  const mine = me !== undefined && message.senderId === me.id;
  const visible = openChatId === chatId && !document.hidden;
  const list = queryClient.getQueryData<ChatSummary[]>(chatsKey);
  if (!list) {
    void queryClient.invalidateQueries({ queryKey: chatsKey });
  } else {
    queryClient.setQueryData<ChatSummary[]>(chatsKey, noteIncoming(list, chatId, message, { mine, visible }));
  }
  const existing = queryClient.getQueryData<MessageCache>(messagesKey(chatId));
  if (!existing) {
    if (visible) void queryClient.invalidateQueries({ queryKey: messagesKey(chatId) });
    return;
  }
  queryClient.setQueryData<MessageCache>(messagesKey(chatId), ingestMessage(existing, message));
}
