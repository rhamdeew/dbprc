import { useEffect, useRef } from 'react';
import type { ChatSummary } from '../../api/chat';
import type { RealtimeEvent } from '../../api/realtime';

const appTitle = 'ARC';

/** How many Unicode code points of a message fit in a browser notification. */
const notificationPreviewLength = 80;

/** `(3) ARC` when something is unread, otherwise `ARC`. */
function tabTitle(unread: number): string {
  if (unread > 0) return `(${unread}) ${appTitle}`;
  return appTitle;
}

function totalUnread(chats: { unreadCount: number }[]): number {
  return chats.reduce((sum, chat) => sum + chat.unreadCount, 0);
}

/** First 80 Unicode code points. Shorter text is returned unchanged. */
function previewText(text: string): string {
  const chars = Array.from(text);
  if (chars.length <= notificationPreviewLength) return text;
  return chars.slice(0, notificationPreviewLength).join('');
}

/**
 * Sets the tab title from the loaded chat list and restores the previous title on leave.
 * `chats` is omitted until `GET /api/chats` has resolved.
 */
export function useChatListTitle(chats: ChatSummary[] | undefined) {
  const original = useRef<string | null>(null);
  const unread = chats ? totalUnread(chats) : null;

  useEffect(() => {
    if (original.current === null) original.current = document.title;
    if (unread === null) return;
    document.title = tabTitle(unread);
    return () => {
      if (original.current !== null) document.title = original.current;
    };
  }, [unread]);
}

/**
 * Browser notification for a realtime event. Permission is never requested here:
 * the button is the only caller of `Notification.requestPermission`.
 * A `message.new` whose chat is not in the list is skipped (no partner name yet).
 */
export function notifyIncoming(
  event: RealtimeEvent,
  chats: ChatSummary[] | undefined,
  openChat: (chatId: number) => void,
): void {
  if (typeof Notification === 'undefined') return;
  if (Notification.permission !== 'granted') return;
  if (!document.hidden) return;
  const note = noteFor(event, chats);
  if (!note) return;
  const notification = new Notification(note.title, { body: note.body });
  notification.onclick = () => {
    window.focus();
    openChat(note.chatId);
    notification.close();
  };
}

function noteFor(
  event: RealtimeEvent,
  chats: ChatSummary[] | undefined,
): { title: string; body: string; chatId: number } | null {
  if (event.type === 'chat.new') {
    return {
      title: event.data.partner.name,
      body: previewText(event.data.lastMessage?.body ?? ''),
      chatId: event.data.id,
    };
  }
  const partner = chats?.find((chat) => chat.id === event.data.chatId)?.partner.name;
  if (partner === undefined) return null;
  return { title: partner, body: previewText(event.data.message.body), chatId: event.data.chatId };
}
