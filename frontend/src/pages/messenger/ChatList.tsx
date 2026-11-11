import { Avatar, Badge, Group, Stack, Text } from '@mantine/core';
import { NavLink } from 'react-router';
import type { ChatSummary } from '../../api/chat';
import { BORDER } from '../../theme/theme';

type ChatListProps = {
  chats: ChatSummary[];
  activeId: number | null;
};

export function ChatList({ chats, activeId }: ChatListProps) {
  if (chats.length === 0) {
    return <Text c="dimmed">No chats yet. Start a random one.</Text>;
  }

  return (
    <Stack component="nav" aria-label="Chats" gap={0}>
      {chats.map((chat) => (
        <NavLink
          key={chat.id}
          to={`/messenger/${chat.id}`}
          aria-current={chat.id === activeId ? 'page' : undefined}
          style={({ isActive }) => ({
            display: 'block',
            textDecoration: 'none',
            color: 'inherit',
            background: isActive ? '#e7ecf5' : 'transparent',
            padding: '8px',
            borderTop: `1px solid ${BORDER}`,
          })}
        >
          <Group wrap="nowrap" gap="sm" align="flex-start">
            <Avatar src={chat.partner.avatarUrl} name={chat.partner.name} alt="" size={36} radius={2} color="arcBlue" />
            <Stack gap={2} style={{ flex: 1, minWidth: 0 }}>
              <Text fw={700} lineClamp={1}>
                {chat.partner.name}
              </Text>
              <Text c="dimmed" size="xs" lineClamp={1}>
                {chat.lastMessage?.body ?? 'No messages yet'}
              </Text>
            </Stack>
            {chat.unreadCount > 0 ? (
              <Badge color="arcBlue" variant="filled" aria-label={`${chat.unreadCount} unread`} style={{ flexShrink: 0 }}>
                {chat.unreadCount}
              </Badge>
            ) : null}
          </Group>
        </NavLink>
      ))}
    </Stack>
  );
}
