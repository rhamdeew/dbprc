import { useQueryClient } from '@tanstack/react-query';
import { useMatch, useNavigate } from 'react-router';
import { chatsKey, parseChatId, useChats, type ChatSummary } from '../api/chat';
import { useRealtime } from '../api/realtime';
import { notifyIncoming, useChatListTitle } from '../pages/messenger/notifications';

/**
 * Keeps chats live on every signed-in page: one socket, the unread count in the
 * tab title, and browser notifications. It renders nothing. Mount it once, in the shell.
 */
export function LiveChats() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const chats = useChats();
  // The chat that is on screen, or null on every other page.
  const openChatId = parseChatId(useMatch('/messenger/:chatId')?.params.chatId);
  useChatListTitle(chats.data);
  useRealtime(openChatId, (event) => {
    notifyIncoming(event, queryClient.getQueryData<ChatSummary[]>(chatsKey), (id) => {
      navigate(`/messenger/${id}`);
    });
  });
  return null;
}
