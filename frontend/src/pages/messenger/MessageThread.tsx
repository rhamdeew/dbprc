import { Anchor, Avatar, Box, Button, Center, Group, Loader, Stack, Text, Textarea, Title } from '@mantine/core';
import { Fragment, useLayoutEffect, useRef, useState, type FormEvent, type KeyboardEvent, type UIEvent } from 'react';
import { Link } from 'react-router';
import { ApiError } from '../../api/client';
import {
  flattenMessages,
  latestServerMessageId,
  useMarkThreadRead,
  useMessages,
  useSendMessage,
  type ChatMessage,
  type ChatPartner,
} from '../../api/chat';
import { useMe } from '../../api/me';
import { BORDER, BUBBLE, BUBBLE_BORDER, PANEL } from '../../theme/theme';
import { clockTime, dayLabel } from './time';

const olderThreshold = 48;
const bubbleAvatar = 26;
/** What the API sends in place of a body it could not decrypt. */
const unavailableBody = '[message unavailable]';

type MessageThreadProps = {
  chatId: number;
  /** Null until the chat list has this chat. */
  partner: ChatPartner | null;
};

function PartnerAvatar({ partner, size }: { partner: ChatPartner | null; size: number }) {
  return (
    <Avatar
      src={partner?.avatarUrl}
      name={partner?.name}
      alt=""
      size={size}
      radius="50%"
      color="arcBlue"
      style={{ border: `1px solid ${BORDER}`, flexShrink: 0 }}
    />
  );
}

/** A centered line between two hairlines: a day label or a system message. */
function Separator({ kind, children }: { kind: 'day' | 'system'; children: string }) {
  const rule = <Box style={{ flex: 1, borderTop: `1px solid ${BORDER}` }} />;
  return (
    <Group gap={8} wrap="nowrap" my={4}>
      {rule}
      <Text
        size="xs"
        c="dimmed"
        fw={kind === 'day' ? 700 : undefined}
        data-kind={kind}
        style={kind === 'system' ? { fontStyle: 'italic' } : undefined}
      >
        {children}
      </Text>
      {rule}
    </Group>
  );
}

type BubbleProps = {
  message: ChatMessage;
  mine: boolean;
  /** Last line of a run from one sender: it gets the avatar and the squared corner. */
  last: boolean;
  partner: ChatPartner | null;
};

function Bubble({ message, mine, last, partner }: BubbleProps) {
  const broken = message.body === unavailableBody;
  // A negative id is the optimistic copy that the server has not confirmed yet.
  const pending = message.id < 0;
  return (
    <Group justify={mine ? 'flex-end' : 'flex-start'} align="flex-end" gap={6} wrap="nowrap" mb={last ? 6 : 0}>
      {mine ? null : last ? (
        <PartnerAvatar partner={partner} size={bubbleAvatar} />
      ) : (
        <Box w={bubbleAvatar} style={{ flexShrink: 0 }} />
      )}
      <Box
        maw="75%"
        px={9}
        py={5}
        style={{
          background: mine ? BUBBLE : '#ffffff',
          border: `1px solid ${mine ? BUBBLE_BORDER : BORDER}`,
          borderRadius: 10,
          ...(last ? (mine ? { borderBottomRightRadius: 2 } : { borderBottomLeftRadius: 2 }) : {}),
          opacity: pending ? 0.6 : 1,
          minWidth: 0,
        }}
      >
        <Text
          c={broken ? 'dimmed' : undefined}
          style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', fontStyle: broken ? 'italic' : undefined }}
        >
          {broken ? 'Message unavailable' : message.body}
        </Text>
        <Text component="time" dateTime={message.createdAt} display="block" c="dimmed" size="xs" ta="right">
          {pending ? 'sending…' : clockTime(message.createdAt)}
        </Text>
      </Box>
    </Group>
  );
}

function MessageList({ items, myId, partner }: { items: ChatMessage[]; myId: number | undefined; partner: ChatPartner | null }) {
  return (
    <Stack gap={3}>
      {items.map((message, index) => {
        const previous = items[index - 1];
        const next = items[index + 1];
        const day = dayLabel(message.createdAt);
        const newDay = day !== '' && (!previous || dayLabel(previous.createdAt) !== day);
        const last =
          !next || next.kind === 'system' || next.senderId !== message.senderId || dayLabel(next.createdAt) !== day;
        return (
          <Fragment key={message.id}>
            {newDay ? <Separator kind="day">{day}</Separator> : null}
            {message.kind === 'system' ? (
              <Separator kind="system">{message.body}</Separator>
            ) : (
              <Bubble message={message} mine={message.senderId === myId} last={last} partner={partner} />
            )}
          </Fragment>
        );
      })}
    </Stack>
  );
}

export function MessageThread({ chatId, partner }: MessageThreadProps) {
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
    <Stack gap={0}>
      <Group gap={8} px={8} py={6} wrap="nowrap" style={{ borderBottom: `1px solid ${BORDER}`, background: PANEL }}>
        <PartnerAvatar partner={partner} size={30} />
        {partner ? (
          <Group gap={4} wrap="nowrap" style={{ minWidth: 0 }}>
            <Text style={{ flexShrink: 0 }}>Chat with</Text>
            <Title order={2} fz={12} lh={1.45} style={{ minWidth: 0, overflowWrap: 'anywhere' }}>
              <Anchor component={Link} to={`/u/${partner.id}`} fw={700} inherit>
                {partner.name}
              </Anchor>
            </Title>
          </Group>
        ) : (
          <Title order={2} fz={12} lh={1.45}>
            Chat
          </Title>
        )}
      </Group>
      {messages.isPending ? (
        <Center mih={280}>
          <Loader color="arcBlue" size="sm" aria-label="Loading messages" />
        </Center>
      ) : messages.isError ? (
        <Center mih={280} p="md">
          <Text>{threadError(messages.error)}</Text>
        </Center>
      ) : (
        <>
          {messages.isFetchingNextPage ? (
            <Text size="xs" c="dimmed" ta="center" py={2}>
              Loading older messages
            </Text>
          ) : null}
          <Box
            ref={scroller}
            role="log"
            aria-label="Messages"
            tabIndex={0}
            onScroll={onScroll}
            px={10}
            py={8}
            h="calc(100dvh - 230px)"
            mih={280}
            style={{ overflowY: 'auto' }}
          >
            {items.length === 0 ? (
              <Center h="100%">
                <Stack gap={6} align="center">
                  <PartnerAvatar partner={partner} size={56} />
                  <Text c="dimmed" ta="center">
                    No messages yet.
                  </Text>
                </Stack>
              </Center>
            ) : (
              <MessageList items={items} myId={me.data?.id} partner={partner} />
            )}
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
    <Box
      component="form"
      aria-label="Message composer"
      onSubmit={onSubmit}
      p={8}
      style={{ borderTop: `1px solid ${BORDER}`, background: PANEL }}
    >
      <Group align="flex-end" gap={6} wrap="nowrap">
        <Textarea
          aria-label="Message"
          placeholder="Write a message…"
          size="sm"
          value={text}
          onChange={(event) => setText(event.currentTarget.value)}
          onKeyDown={onKeyDown}
          autosize
          minRows={1}
          maxRows={6}
          maxLength={4000}
          disabled={send.isPending}
          style={{ flex: 1, minWidth: 0 }}
        />
        <Button type="submit" size="sm" loading={send.isPending} disabled={text.trim() === ''}>
          Send
        </Button>
      </Group>
      <Text size="xs" c="dimmed" mt={4}>
        Enter to send · Shift+Enter for a new line
      </Text>
    </Box>
  );
}
