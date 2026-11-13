import { Avatar, Badge, Group, Stack, Text } from '@mantine/core';
import { NavLink } from 'react-router';
import type { ChatSummary } from '../../api/chat';
import { BORDER, HEADER, PANEL } from '../../theme/theme';

type ChatListProps = {
  chats: ChatSummary[];
  activeId: number | null;
};

export function ChatList({ chats, activeId }: ChatListProps) {
  if (chats.length === 0) {
    return (
      <Text c="dimmed" ta="center" p="sm">
        No chats yet. Start a random one.
      </Text>
    );
  }

  return (
    <Stack component="nav" aria-label="Chats" gap={0}>
      {chats.map((chat, index) => {
        const unread = chat.unreadCount > 0;
        return (
          <NavLink
            key={chat.id}
            to={`/messenger/${chat.id}`}
            aria-current={chat.id === activeId ? 'page' : undefined}
            className="arc-chat-row"
            style={({ isActive }) => ({
              display: 'block',
              textDecoration: 'none',
              color: 'inherit',
              padding: '7px 8px',
              borderTop: index > 0 ? `1px solid ${BORDER}` : undefined,
              // An inline background would beat the :hover rule in base.css, so only the open chat sets one.
              ...(isActive ? { background: PANEL, boxShadow: `inset 3px 0 0 ${HEADER}` } : {}),
            })}
          >
            {({ isActive }) => (
              <Group wrap="nowrap" gap={8}>
                <Avatar
                  src={chat.partner.avatarUrl}
                  name={chat.partner.name}
                  alt=""
                  size={34}
                  radius="50%"
                  color="arcBlue"
                  style={{ border: `1px solid ${BORDER}`, flexShrink: 0 }}
                />
                <Stack gap={0} style={{ flex: 1, minWidth: 0 }}>
                  <Text fw={unread || isActive ? 700 : 400} c={HEADER} truncate>
                    {chat.partner.name}
                  </Text>
                  <Text c={unread ? undefined : 'dimmed'} size="xs" truncate>
                    {chat.lastMessage?.body ?? 'No messages yet'}
                  </Text>
                </Stack>
                {unread ? (
                  <Badge
                    color="red"
                    variant="filled"
                    size="sm"
                    radius="xl"
                    aria-label={`${chat.unreadCount} unread`}
                    style={{ flexShrink: 0 }}
                  >
                    {chat.unreadCount}
                  </Badge>
                ) : null}
              </Group>
            )}
          </NavLink>
        );
      })}
    </Stack>
  );
}
