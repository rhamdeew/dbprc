import { Alert, Box, Button, Flex, Loader, Stack, Switch, Text, Title, Tooltip } from '@mantine/core';
import { notifications } from '@mantine/notifications';
import { useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { useNavigate, useParams } from 'react-router';
import { ApiError } from '../api/client';
import { parseChatId, useChats, useSetAvailability, useStartRandomChat } from '../api/chat';
import { meKey, useMe, type Me } from '../api/me';
import { useRealtime } from '../api/realtime';
import { Panel } from '../components/Panel';
import { ChatList } from './messenger/ChatList';
import { MessageThread } from './messenger/MessageThread';

const unavailableHint = 'Turn availability on to start a random chat.';
const nobodyNotice = 'Nobody is available for a random chat right now. Try again in a little while.';

export function MessengerPage() {
  const params = useParams();
  const chatId = parseChatId(params.chatId);
  const invalidId = params.chatId !== undefined && chatId === null;
  const navigate = useNavigate();
  const me = useMe();
  const chats = useChats();
  const availability = useSetAvailability();
  const random = useStartRandomChat();
  const queryClient = useQueryClient();
  const [notice, setNotice] = useState<string | null>(null);
  useRealtime(chatId);

  const available = me.data?.available ?? false;
  const showUnavailable = me.data !== undefined && !available;
  const partner = chats.data?.find((chat) => chat.id === chatId)?.partner.name ?? null;

  function startChat() {
    if (!available || random.isPending) return;
    setNotice(null);
    random.mutate(undefined, {
      onSuccess: (result) => {
        navigate(`/messenger/${result.chat.id}`);
      },
      onError: (error) => {
        if (error instanceof ApiError && error.code === 'nobody_available') {
          setNotice(nobodyNotice);
          return;
        }
        if (error instanceof ApiError && error.code === 'you_are_unavailable') {
          queryClient.setQueryData<Me>(meKey, (current) => (current ? { ...current, available: false } : current));
          setNotice(error.message);
          return;
        }
        const message = error instanceof ApiError && error.message !== '' ? error.message : 'Something went wrong';
        notifications.show({ color: 'red', message });
      },
    });
  }

  const startButton = (
    <Button type="button" disabled={!available || random.isPending} loading={random.isPending} onClick={startChat}>
      Start random chat ☕
    </Button>
  );

  return (
    <Flex direction={{ base: 'column', sm: 'row' }} gap="md" align="stretch">
      <Box w={{ base: '100%', sm: 300 }} style={{ flexShrink: 0 }}>
        <Panel>
          <Stack gap="sm">
            <Title order={1}>Messenger</Title>
            {showUnavailable ? (
              <Tooltip
                label={unavailableHint}
                position="top"
                openDelay={0}
                closeDelay={0}
                withinPortal={false}
                hideDetached={false}
                transitionProps={{ duration: 0 }}
                events={{ hover: true, focus: true, touch: false }}
              >
                <Box
                  component="span"
                  tabIndex={0}
                  aria-label={unavailableHint}
                  style={{ display: 'inline-block', alignSelf: 'flex-start' }}
                >
                  {startButton}
                </Box>
              </Tooltip>
            ) : (
              startButton
            )}
            {notice ? (
              <Alert color="arcBlue" variant="light" role="status">
                {notice}
              </Alert>
            ) : null}
            <Switch
              label="Available for random chats"
              checked={available}
              disabled={availability.isPending || !me.data}
              onChange={(event) => {
                setNotice(null);
                availability.mutate(event.currentTarget.checked);
              }}
            />
            {chats.isPending ? <Loader color="arcBlue" aria-label="Loading chats" /> : null}
            {chats.isError ? <Text>{chats.error instanceof ApiError ? chats.error.message : 'Could not load chats.'}</Text> : null}
            {chats.data ? <ChatList chats={chats.data} activeId={chatId} /> : null}
          </Stack>
        </Panel>
      </Box>
      <Box style={{ flex: '1 1 auto', minWidth: 0 }}>
        <Panel>
          {invalidId ? (
            <Text>Chat not found.</Text>
          ) : chatId === null ? (
            <Text c="dimmed">Choose a chat, or start a random one.</Text>
          ) : (
            <MessageThread chatId={chatId} partnerName={partner} />
          )}
        </Panel>
      </Box>
    </Flex>
  );
}
