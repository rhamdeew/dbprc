import { Box, Button, Group, Loader, Stack, Text, Textarea, Title } from '@mantine/core';
import { useLayoutEffect, useRef, useState, type FormEvent, type KeyboardEvent, type UIEvent } from 'react';
import { ApiError } from '../../api/client';
import {
  flattenMessages,
  latestServerMessageId,
  useMarkThreadRead,
  useMessages,
  useSendMessage,
} from '../../api/chat';
import { useMe } from '../../api/me';
import { BORDER } from '../../theme/theme';
import { relativeTime } from '../profile/relativeTime';

const olderThreshold = 48;

type MessageThreadProps = {
  chatId: number;
  partnerName: string | null;
};

export function MessageThread({ chatId, partnerName }: MessageThreadProps) {
  const me = useMe();
  const messages = useMessages(chatId);
  const items = flattenMessages(messages.data);
  useMarkThreadRead(chatId, latestServerMessageId(items));

  const scroller = useRef<HTMLDivElement>(null);
  const anchorHeight = useRef<number | null>(null);
  const stickToBottom = useRef(true);
  const head = items[0]?.id ?? 0;
  const tail = items[items.length - 1]?.id ?? 0;

  useLayoutEffect(() => {
    const el = scroller.current;
    if (!el) return;
    if (anchorHeight.current !== null) {
      const previous = anchorHeight.current;
      anchorHeight.current = null;
      el.scrollTop += el.scrollHeight - previous;
      return;
    }
    if (stickToBottom.current) el.scrollTop = el.scrollHeight;
  }, [head, tail, items.length]);

  function onScroll(event: UIEvent<HTMLDivElement>) {
    const el = event.currentTarget;
    const distance = el.scrollHeight - el.scrollTop - el.clientHeight;
    stickToBottom.current = distance < 80;
    if (el.scrollTop > olderThreshold) return;
    if (el.scrollHeight <= el.clientHeight) return;
    if (!messages.hasNextPage || messages.isFetchingNextPage) return;
    anchorHeight.current = el.scrollHeight;
    stickToBottom.current = false;
    void messages.fetchNextPage();
  }

  return (
    <Stack gap="sm">
      <Title order={2}>{partnerName ?? 'Chat'}</Title>
      {messages.isPending ? (
        <Loader color="arcBlue" aria-label="Loading messages" />
      ) : messages.isError ? (
        <Text>{threadError(messages.error)}</Text>
      ) : (
        <>
          {messages.isFetchingNextPage ? <Text size="xs">Loading older messages</Text> : null}
          <Box
            ref={scroller}
            role="log"
            aria-label="Messages"
            tabIndex={0}
            onScroll={onScroll}
            style={{ overflowY: 'auto', maxHeight: 480, minHeight: 240 }}
          >
            <Stack gap="xs">
              {items.length === 0 ? (
                <Text c="dimmed">No messages yet.</Text>
              ) : (
                items.map((message) => (
                  <Box key={message.id}>
                    {message.kind === 'system' ? (
                      <Text ta="center" c="dimmed" data-kind="system" style={{ fontStyle: 'italic' }}>
                        {message.body}
                      </Text>
                    ) : (
                      <Box
                        maw="80%"
                        ml={message.senderId === me.data?.id ? 'auto' : undefined}
                        p="xs"
                        bg={message.senderId === me.data?.id ? '#e7ecf5' : 'white'}
                        style={{ border: `1px solid ${BORDER}` }}
                      >
                        <Text style={{ whiteSpace: 'pre-wrap' }}>{message.body}</Text>
                        <Text component="time" dateTime={message.createdAt} c="dimmed" size="xs">
                          {relativeTime(message.createdAt)}
                        </Text>
                      </Box>
                    )}
                  </Box>
                ))
              )}
            </Stack>
          </Box>
          <Composer
            chatId={chatId}
            onSending={() => {
              stickToBottom.current = true;
            }}
          />
        </>
      )}
    </Stack>
  );
}

function threadError(error: unknown): string {
  if (error instanceof ApiError && error.message !== '') return error.message;
  return 'Could not load this chat.';
}

function Composer({ chatId, onSending }: { chatId: number; onSending: () => void }) {
  const send = useSendMessage(chatId);
  const [text, setText] = useState('');

  function submit() {
    const body = text.trim();
    if (body === '' || send.isPending) return;
    const previous = text;
    setText('');
    onSending();
    send.mutate(body, { onError: () => setText(previous) });
  }

  function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    submit();
  }

  function onKeyDown(event: KeyboardEvent<HTMLTextAreaElement>) {
    if (event.key !== 'Enter' || event.shiftKey || event.nativeEvent.isComposing) return;
    event.preventDefault();
    submit();
  }

  return (
    <form aria-label="Message composer" onSubmit={onSubmit}>
      <Stack gap="xs">
        <Textarea
          label="Message"
          value={text}
          onChange={(event) => setText(event.currentTarget.value)}
          onKeyDown={onKeyDown}
          minRows={2}
          maxRows={6}
          maxLength={4000}
          disabled={send.isPending}
        />
        <Group justify="flex-end">
          <Button type="submit" loading={send.isPending} disabled={text.trim() === ''}>
            Send
          </Button>
        </Group>
      </Stack>
    </form>
  );
}
