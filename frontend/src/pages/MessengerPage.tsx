import { Alert, Box, Button, Center, Divider, Flex, Loader, Stack, Switch, Text, Title, Tooltip, VisuallyHidden } from '@mantine/core';
import { notifications } from '@mantine/notifications';
import { useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { useNavigate, useParams } from 'react-router';
import { ApiError } from '../api/client';
import { parseChatId, useAvailableCount, useChats, useSetAvailability, useStartRandomChat } from '../api/chat';
import { meKey, useMe, type Me } from '../api/me';
import { Panel } from '../components/Panel';
import { BEVEL, BORDER, HEADER } from '../theme/theme';
import { ChatList } from './messenger/ChatList';
import { EnableNotificationsButton } from './messenger/EnableNotificationsButton';
import { MessageThread } from './messenger/MessageThread';

const unavailableHint = 'Turn availability on to start a random chat.';
const nobodyNotice = 'Nobody is available for a random chat right now. Try again in a little while.';
/** The count is shown only above this number, so a small pool never looks empty or hints at who is in it. */
export const minShownAvailable = 10;

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

  const available = me.data?.available ?? false;
  const showUnavailable = me.data !== undefined && !available;
  const partner = chats.data?.find((chat) => chat.id === chatId)?.partner ?? null;
  const availableCount = useAvailableCount().data ?? 0;

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
    <Button
      type="button"
      size="sm"
      fullWidth
      disabled={!available || random.isPending}
      loading={random.isPending}
      onClick={startChat}
      style={available ? BEVEL : undefined}
    >
      Start random chat ☕
    </Button>
  );

  return (
    <>
      <VisuallyHidden>
        <Title order={1}>Messenger</Title>
      </VisuallyHidden>
      <Flex direction={{ base: 'column', sm: 'row' }} gap="md" align="flex-start">
        <Stack gap="md" w={{ base: '100%', sm: 280 }} style={{ flexShrink: 0 }}>
          <Panel title="Random coffee">
            <Stack gap={8}>
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
                  <Box component="span" tabIndex={0} aria-label={unavailableHint} style={{ display: 'block' }}>
                    {startButton}
                  </Box>
                </Tooltip>
              ) : (
                startButton
              )}
              {available && availableCount > minShownAvailable ? (
                <Text size="xs" c="dimmed" ta="center">
                  <Text span fw={700} c={HEADER} inherit>
                    {availableCount}
                  </Text>{' '}
                  colleagues are available right now
                </Text>
              ) : null}
              {notice ? (
                <Alert color="arcBlue" variant="light" p={8} role="status">
                  {notice}
                </Alert>
              ) : null}
              <Divider color={BORDER} />
              <Switch
                size="md"
                onLabel="ON"
                offLabel="OFF"
                label="Available for random chats"
                // The ON/OFF track text would otherwise become part of the accessible name.
                aria-label="Available for random chats"
                description={
                  available
                    ? 'You are in the pool: any colleague can be matched with you, and you can start a random chat.'
                    : 'You are out of the pool: nobody new gets matched with you. Your existing chats keep working.'
                }
                checked={available}
                disabled={availability.isPending || !me.data}
                onChange={(event) => {
                  setNotice(null);
                  availability.mutate(event.currentTarget.checked);
                }}
                styles={{ label: { fontWeight: 700 }, body: { alignItems: 'flex-start' } }}
              />
            </Stack>
          </Panel>
          <Panel title="Chats" action={<EnableNotificationsButton />} p={0}>
            {chats.isPending ? (
              <Center p="sm">
                <Loader color="arcBlue" size="sm" aria-label="Loading chats" />
              </Center>
            ) : null}
            {chats.isError ? (
              <Text p="sm">{chats.error instanceof ApiError ? chats.error.message : 'Could not load chats.'}</Text>
            ) : null}
            {chats.data ? <ChatList chats={chats.data} activeId={chatId} /> : null}
          </Panel>
        </Stack>
        <Box w={{ base: '100%', sm: 'auto' }} bg="white" style={{ flex: '1 1 auto', minWidth: 0, border: `1px solid ${BORDER}` }}>
          {invalidId ? (
            <Center mih={300} p="md">
              <Text>Chat not found.</Text>
            </Center>
          ) : chatId === null ? (
            <Center mih={300} p="md">
              <Stack gap={4} align="center">
                <Text fz={32} lh={1} aria-hidden="true">
                  ☕
                </Text>
                <Text c="dimmed" ta="center">
                  Choose a chat, or start a random one.
                </Text>
              </Stack>
            </Center>
          ) : (
            <MessageThread key={chatId} chatId={chatId} partner={partner} />
          )}
        </Box>
      </Flex>
    </>
  );
}
