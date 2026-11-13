import { act, fireEvent, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { messagePageSize, type ChatMessage, type ChatSummary } from '../api/chat';
import { reconnectClock } from '../api/realtime';
import { LiveChats } from '../components/LiveChats';
import { member } from '../test/fixtures';
import { render } from '../test/render';
import { chatSocket, server } from '../test/server';
import { MessengerPage } from './MessengerPage';

const unavailableHint = 'Turn availability on to start a random chat.';
const nobodyNotice = 'Nobody is available for a random chat right now. Try again in a little while.';

function line(id: number, body: string, senderId: number | null = 3, kind = 'text'): ChatMessage {
  return { id, senderId, kind, body, createdAt: '2026-11-11T12:00:00Z' };
}

function chat(id: number, name: string, unreadCount: number, last: string | null, avatarUrl: string | null = null): ChatSummary {
  return {
    id,
    partner: { id: id + 100, name, avatarUrl },
    lastMessage: last === null ? null : { body: last, kind: 'text', createdAt: '2026-11-11T12:00:00Z' },
    unreadCount,
  };
}

function LocationEcho() {
  const location = useLocation();
  return <div data-testid="location">{location.pathname}</div>;
}

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <LocationEcho />
      {/* In the app the shell mounts this: the socket, the tab title, and notifications. */}
      <LiveChats />
      <Routes>
        <Route path="/messenger/:chatId?" element={<MessengerPage />} />
      </Routes>
    </MemoryRouter>,
  );
}

// The thread header also links the partner's name (to their profile), so chat rows are looked up inside the list.
function chatNav() {
  return within(screen.getByRole('navigation', { name: 'Chats' }));
}

function chatRow(name: RegExp) {
  return chatNav().getByRole('link', { name });
}

async function findChatRow(name: RegExp) {
  return within(await screen.findByRole('navigation', { name: 'Chats' })).findByRole('link', { name });
}

const clients: { send: (data: string) => void; close: () => void }[] = [];

function push(data: unknown) {
  const client = clients.at(-1);
  if (!client) throw new Error('socket was not opened');
  client.send(typeof data === 'string' ? data : JSON.stringify(data));
}

type PostedNotification = {
  title: string;
  body: string;
  onclick: ((event: Event) => void) | null;
  close: ReturnType<typeof vi.fn>;
};

function installNotification(permission: NotificationPermission, next: NotificationPermission = 'granted') {
  const posted: PostedNotification[] = [];
  const requestPermission = vi.fn(async () => next);
  class FakeNotification {
    static permission = permission;
    static requestPermission = requestPermission;
    title: string;
    body: string;
    onclick: ((event: Event) => void) | null = null;
    close = vi.fn();
    constructor(title: string, options?: { body?: string }) {
      this.title = title;
      this.body = options?.body ?? '';
      posted.push(this);
    }
  }
  vi.stubGlobal('Notification', FakeNotification);
  return { posted, requestPermission };
}

function setDocumentHidden(hidden: boolean) {
  Object.defineProperty(document, 'hidden', { configurable: true, get: () => hidden });
}

function restoreDocumentHidden() {
  Reflect.deleteProperty(document, 'hidden');
}

describe('messenger', () => {
  beforeEach(() => {
    clients.length = 0;
    server.use(
      chatSocket.addEventListener('connection', ({ client }) => {
        clients.push(client);
      }),
      http.get('/api/me', () => HttpResponse.json(member)),
      http.get('/api/chats', () => HttpResponse.json([])),
      http.get('/api/chats/:id/messages', () => HttpResponse.json({ messages: [] })),
      http.post('/api/chats/:id/read', () => HttpResponse.json({ lastMessageId: 1 })),
    );
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
    restoreDocumentHidden();
    document.title = '';
  });

  it('starts a random chat and opens it', async () => {
    const user = userEvent.setup();
    const created = chat(9, 'Katherine Johnson', 0, null, '/uploads/avatars/kj.png');
    server.use(
      http.post('/api/chats/random', () => HttpResponse.json({ chat: created, rematch: false }, { status: 201 })),
    );
    renderAt('/messenger');

    const button = await screen.findByRole('button', { name: 'Start random chat ☕' });
    await waitFor(() => expect(button).toBeEnabled());
    await user.click(button);

    expect(await screen.findByRole('heading', { name: 'Katherine Johnson' })).toBeInTheDocument();
    expect(screen.getByTestId('location')).toHaveTextContent('/messenger/9');
    expect(chatRow(/Katherine Johnson/)).toBeInTheDocument();
    expect(document.querySelector('img')).toHaveAttribute('src', '/uploads/avatars/kj.png');
    expect(await screen.findByText('No messages yet.')).toBeInTheDocument();
  });

  it('shows a notice when nobody is available', async () => {
    const user = userEvent.setup();
    server.use(
      http.post('/api/chats/random', () =>
        HttpResponse.json({ code: 'nobody_available', message: 'Nobody is available right now.' }, { status: 404 }),
      ),
    );
    renderAt('/messenger');

    const button = await screen.findByRole('button', { name: 'Start random chat ☕' });
    await waitFor(() => expect(button).toBeEnabled());
    await user.click(button);

    expect(await screen.findByRole('status')).toHaveTextContent(nobodyNotice);
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    expect(screen.getByTestId('location').textContent).toBe('/messenger');
    expect(screen.getByText('Choose a chat, or start a random one.')).toBeInTheDocument();
  });

  it('disables the button and explains why, then follows the availability switch', async () => {
    const user = userEvent.setup();
    const puts: unknown[] = [];
    server.use(
      http.get('/api/me', () => HttpResponse.json({ ...member, available: false })),
      http.put('/api/me/availability', async ({ request }) => {
        const body = await request.json();
        puts.push(body);
        return HttpResponse.json(body);
      }),
    );
    renderAt('/messenger');

    const toggle = await screen.findByRole('switch', { name: 'Available for random chats' });
    await waitFor(() => expect(toggle).toBeEnabled());
    expect(toggle).not.toBeChecked();
    const button = screen.getByRole('button', { name: 'Start random chat ☕' });
    expect(button).toBeDisabled();
    const target = button.parentElement;
    if (!target) throw new Error('missing tooltip target');
    expect(target).toHaveAccessibleName(unavailableHint);
    await user.hover(target);
    expect(await screen.findByRole('tooltip')).toHaveTextContent(unavailableHint);

    await user.click(toggle);
    await waitFor(() => expect(puts).toContainEqual({ available: true }));
    await waitFor(() => expect(screen.getByRole('button', { name: 'Start random chat ☕' })).toBeEnabled());
    expect(screen.getByRole('switch', { name: 'Available for random chats' })).toBeChecked();

    await user.click(screen.getByRole('switch', { name: 'Available for random chats' }));
    await waitFor(() => expect(puts).toContainEqual({ available: false }));
    expect(screen.getByRole('button', { name: 'Start random chat ☕' })).toBeDisabled();
  });

  it('turns the switch off when the server says you are unavailable', async () => {
    const user = userEvent.setup();
    server.use(
      http.post('/api/chats/random', () =>
        HttpResponse.json(
          { code: 'you_are_unavailable', message: 'Turn availability on to start a random chat.' },
          { status: 409 },
        ),
      ),
    );
    renderAt('/messenger');
    const button = await screen.findByRole('button', { name: 'Start random chat ☕' });
    await waitFor(() => expect(button).toBeEnabled());
    await user.click(button);

    expect(await screen.findByRole('status')).toHaveTextContent('Turn availability on to start a random chat.');
    expect(screen.getByRole('button', { name: 'Start random chat ☕' })).toBeDisabled();
    expect(screen.getByRole('switch', { name: 'Available for random chats' })).not.toBeChecked();
  });

  it.each([
    [11, true, true],
    [10, true, false],
    [25, false, false],
  ])('with %i others available and me available=%s, the count line shown is %s', async (count, available, shown) => {
    let asked = false;
    server.use(
      http.get('/api/me', () => HttpResponse.json({ ...member, available })),
      http.get('/api/chats/available', () => {
        asked = true;
        return HttpResponse.json({ count });
      }),
    );
    renderAt('/messenger');

    const toggle = await screen.findByRole('switch', { name: 'Available for random chats' });
    await waitFor(() => expect(toggle).toBeEnabled());
    await waitFor(() => expect(asked).toBe(true));
    if (shown) {
      const line = await screen.findByText(/colleagues are available right now/);
      expect(line).toHaveTextContent(`${count} colleagues are available right now`);
    } else {
      // Let the response land before asserting that nothing appeared.
      await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 20));
      });
      expect(screen.queryByText(/colleagues are available right now/)).not.toBeInTheDocument();
    }
  });

  it('hides the count line when the count request fails', async () => {
    let asked = false;
    server.use(
      http.get('/api/chats/available', () => {
        asked = true;
        return HttpResponse.json({ code: 'internal', message: 'Something went wrong.' }, { status: 500 });
      }),
    );
    renderAt('/messenger');

    await waitFor(() => expect(asked).toBe(true));
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 20));
    });
    expect(screen.queryByText(/colleagues are available right now/)).not.toBeInTheDocument();
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('links the thread header to the partner, separates days, and dims an undecryptable line', async () => {
    const yesterday = new Date(Date.now() - 24 * 60 * 60_000).toISOString();
    const today = new Date().toISOString();
    server.use(
      http.get('/api/chats', () => HttpResponse.json([chat(5, 'Grace Hopper', 0, 'hi')])),
      http.get('/api/chats/:id/messages', () =>
        HttpResponse.json({
          messages: [
            { ...line(1, '[message unavailable]'), createdAt: yesterday },
            { ...line(2, 'hi'), createdAt: today },
          ],
        }),
      ),
    );
    renderAt('/messenger/5');

    const heading = await screen.findByRole('heading', { level: 2, name: 'Grace Hopper' });
    expect(within(heading).getByRole('link', { name: 'Grace Hopper' })).toHaveAttribute('href', '/u/105');
    const log = await screen.findByRole('log', { name: 'Messages' });
    const broken = await within(log).findByText('Message unavailable');
    expect(broken).toHaveStyle({ fontStyle: 'italic' });
    expect(within(log).queryByText('[message unavailable]')).not.toBeInTheDocument();
    const days = Array.from(log.querySelectorAll('[data-kind="day"]')).map((node) => node.textContent);
    expect(days).toEqual(['Yesterday', 'Today']);
  });

  it('marks a message as sending until the server confirms it', async () => {
    const user = userEvent.setup();
    let release: (response: Response) => void = () => {};
    const pending = new Promise<Response>((resolve) => {
      release = resolve;
    });
    server.use(
      http.get('/api/chats', () => HttpResponse.json([chat(5, 'Grace Hopper', 0, null)])),
      http.post('/api/chats/:id/messages', () => pending),
    );
    renderAt('/messenger/5');
    const box = await screen.findByRole('textbox', { name: 'Message' });
    await user.type(box, 'on its way');
    await user.keyboard('{Enter}');

    const log = screen.getByRole('log', { name: 'Messages' });
    expect(await within(log).findByText('sending…')).toBeInTheDocument();
    await act(async () => {
      release(HttpResponse.json(line(9, 'on its way', member.id), { status: 201 }));
    });
    await waitFor(() => expect(within(log).queryByText('sending…')).not.toBeInTheDocument());
    expect(within(log).getByText('on its way')).toBeInTheDocument();
  });

  it('keeps a newline on Shift+Enter and sends the trimmed text on Enter', async () => {
    const user = userEvent.setup();
    const bodies: string[] = [];
    server.use(
      http.get('/api/chats', () => HttpResponse.json([chat(5, 'Grace Hopper', 0, null)])),
      http.post('/api/chats/:id/messages', async ({ request }) => {
        const body = (await request.json()) as { body: string };
        bodies.push(body.body);
        return HttpResponse.json(line(20, body.body, member.id), { status: 201 });
      }),
    );
    renderAt('/messenger/5');
    const box = await screen.findByRole('textbox', { name: 'Message' });

    await user.click(box);
    await user.keyboard('{Enter}');
    await user.type(box, '   ');
    await user.keyboard('{Enter}');
    expect(bodies).toEqual([]);

    await user.clear(box);
    await user.type(box, 'hello');
    await user.keyboard('{Shift>}{Enter}{/Shift}');
    await user.type(box, 'there');
    expect(box).toHaveValue('hello\nthere');
    expect(bodies).toEqual([]);

    await user.keyboard('{Enter}');
    await waitFor(() => expect(bodies).toEqual(['hello\nthere']));
    expect(box).toHaveValue('');
    expect(within(screen.getByRole('log', { name: 'Messages' })).getByText(/hello/)).toBeInTheDocument();
  });

  it('appends a message before the server answers and replaces it once', async () => {
    const user = userEvent.setup();
    let release: (response: Response) => void = () => {};
    const pending = new Promise<Response>((resolve) => {
      release = resolve;
    });
    server.use(
      http.get('/api/chats', () => HttpResponse.json([chat(5, 'Grace Hopper', 0, 'earlier')])),
      http.get('/api/chats/:id/messages', () => HttpResponse.json({ messages: [line(3, 'earlier')] })),
      http.post('/api/chats/:id/messages', () => pending),
    );
    renderAt('/messenger/5');
    const box = await screen.findByRole('textbox', { name: 'Message' });
    await user.type(box, 'Hello there');
    await user.keyboard('{Enter}');

    const log = screen.getByRole('log', { name: 'Messages' });
    expect(within(log).getByText('Hello there')).toBeInTheDocument();
    expect(box).toHaveValue('');
    expect(chatRow(/Grace Hopper/)).toHaveTextContent('Hello there');

    await waitFor(() => expect(clients.length).toBeGreaterThan(0));
    act(() => {
      push({
        type: 'message.new',
        data: { chatId: 5, message: line(9, 'Hello there', member.id) },
      });
    });
    expect(within(log).getAllByText('Hello there')).toHaveLength(1);

    await act(async () => {
      release(HttpResponse.json(line(9, 'Hello there', member.id), { status: 201 }));
    });
    expect(within(log).getAllByText('Hello there')).toHaveLength(1);
  });

  it('puts the draft back when the send fails', async () => {
    const user = userEvent.setup();
    server.use(
      http.get('/api/chats', () => HttpResponse.json([chat(5, 'Grace Hopper', 0, null)])),
      http.post('/api/chats/:id/messages', () =>
        HttpResponse.json({ code: 'invalid', message: 'Write a message first.' }, { status: 400 }),
      ),
    );
    renderAt('/messenger/5');
    const box = await screen.findByRole('textbox', { name: 'Message' });
    await user.type(box, 'nope');
    await user.keyboard('{Enter}');

    expect(await screen.findByRole('alert')).toHaveTextContent('Write a message first.');
    expect(box).toHaveValue('nope');
    expect(within(screen.getByRole('log', { name: 'Messages' })).queryByText('nope')).not.toBeInTheDocument();
  });

  it('loads older messages when the thread is scrolled to the top', async () => {
    const latest = Array.from({ length: messagePageSize }, (_, index) => line(51 + index, `line ${51 + index}`));
    let before: string | null = null;
    server.use(
      http.get('/api/chats', () => HttpResponse.json([chat(5, 'Grace Hopper', 0, 'line 100')])),
      http.get('/api/chats/:id/messages', ({ request }) => {
        const url = new URL(request.url);
        before = url.searchParams.get('before');
        if (before === null) return HttpResponse.json({ messages: latest });
        return HttpResponse.json({ messages: [line(50, 'way back')] });
      }),
    );
    renderAt('/messenger/5');
    const log = await screen.findByRole('log', { name: 'Messages' });
    expect(within(log).getByText('line 100')).toBeInTheDocument();

    Object.defineProperty(log, 'scrollHeight', { configurable: true, value: 800 });
    Object.defineProperty(log, 'clientHeight', { configurable: true, value: 200 });
    Object.defineProperty(log, 'scrollTop', { configurable: true, writable: true, value: 0 });
    fireEvent.scroll(log);

    expect(await screen.findByText('way back')).toBeInTheDocument();
    expect(before).toBe('51');
  });

  it('styles a system line differently from a text line', async () => {
    server.use(
      http.get('/api/chats', () => HttpResponse.json([chat(5, 'Grace Hopper', 0, 'Matched again ☕')])),
      http.get('/api/chats/:id/messages', () =>
        HttpResponse.json({
          messages: [line(1, 'Matched again ☕', null, 'system'), line(2, 'hello', 3)],
        }),
      ),
    );
    renderAt('/messenger/5');
    const system = await within(await screen.findByRole('log', { name: 'Messages' })).findByText('Matched again ☕');
    expect(system).toHaveAttribute('data-kind', 'system');
    expect(system).toHaveStyle({ fontStyle: 'italic' });
    expect(screen.getByText('hello')).not.toHaveAttribute('data-kind', 'system');
  });

  it('applies incoming events to the open thread and the other chat', async () => {
    const reads: unknown[] = [];
    server.use(
      http.get('/api/chats', () =>
        HttpResponse.json([chat(5, 'Grace Hopper', 2, 'earlier'), chat(8, 'Alan Turing', 0, null)]),
      ),
      http.get('/api/chats/:id/messages', ({ params }) => {
        if (params.id === '5') return HttpResponse.json({ messages: [line(3, 'earlier')] });
        return HttpResponse.json({ messages: [] });
      }),
      http.post('/api/chats/:id/read', async ({ request }) => {
        const body = await request.json();
        reads.push(body);
        return HttpResponse.json(body);
      }),
    );
    renderAt('/messenger/5');

    expect(await screen.findByRole('log', { name: 'Messages' })).toHaveTextContent('earlier');
    await waitFor(() => expect(reads).toContainEqual({ lastMessageId: 3 }));
    expect(screen.queryByLabelText(/unread/)).not.toBeInTheDocument();

    await waitFor(() => expect(clients.length).toBeGreaterThan(0));
    act(() => {
      push('not-json');
      push({ type: 'nope', data: {} });
      push({
        type: 'message.new',
        data: { chatId: 5, message: line(4, 'incoming hello', 3) },
      });
    });

    const log = screen.getByRole('log', { name: 'Messages' });
    expect(await within(log).findByText('incoming hello')).toBeInTheDocument();
    expect(chatRow(/Grace Hopper/)).toHaveTextContent('incoming hello');
    await waitFor(() => expect(reads).toContainEqual({ lastMessageId: 4 }));
    expect(screen.queryByLabelText(/unread/)).not.toBeInTheDocument();

    act(() => {
      push({
        type: 'message.new',
        data: { chatId: 8, message: line(6, 'from alan', 4) },
      });
    });
    expect(await screen.findByLabelText('1 unread')).toBeInTheDocument();
    expect(chatRow(/Alan Turing/)).toHaveTextContent('from alan');
    expect(within(log).queryByText('from alan')).not.toBeInTheDocument();

    act(() => {
      push({
        type: 'message.new',
        data: { chatId: 8, message: line(7, 'my other tab', member.id) },
      });
    });
    expect(await screen.findByLabelText('1 unread')).toBeInTheDocument();
    expect(chatRow(/Alan Turing/)).toHaveTextContent('my other tab');

    act(() => {
      push({
        type: 'message.new',
        data: { chatId: 8, message: line(8, 'Matched again ☕', null, 'system') },
      });
    });
    expect(await screen.findByLabelText('2 unread')).toBeInTheDocument();

    act(() => {
      push({
        type: 'chat.new',
        data: chat(11, 'Linus Torvalds', 0, null),
      });
    });
    expect(await findChatRow(/Linus Torvalds/)).toBeInTheDocument();
    expect(chatNav().getAllByRole('link', { name: /Grace Hopper|Alan Turing|Linus Torvalds/ })).toHaveLength(3);
  });

  it('keeps an open hidden thread unread and acknowledges the latest message on return', async () => {
    setDocumentHidden(true);
    const reads: unknown[] = [];
    server.use(
      http.get('/api/chats', () => HttpResponse.json([chat(5, 'Grace Hopper', 1, 'earlier')])),
      http.get('/api/chats/:id/messages', () => HttpResponse.json({ messages: [line(3, 'earlier')] })),
      http.post('/api/chats/:id/read', async ({ request }) => {
        const body = await request.json();
        reads.push(body);
        return HttpResponse.json(body);
      }),
    );
    renderAt('/messenger/5');
    await screen.findByRole('log', { name: 'Messages' });
    await waitFor(() => expect(document.title).toBe('(1) ARC'));
    await waitFor(() => expect(clients.length).toBeGreaterThan(0));

    for (const id of [4, 5]) {
      act(() => push({ type: 'message.new', data: { chatId: 5, message: line(id, `hidden ${id}`) } }));
      await waitFor(() => expect(document.title).toBe(`(${id - 2}) ARC`));
      expect(screen.getByLabelText(`${id - 2} unread`)).toBeInTheDocument();
      expect(reads).toEqual([]);
    }

    act(() => {
      setDocumentHidden(false);
      document.dispatchEvent(new Event('visibilitychange'));
    });
    await waitFor(() => expect(reads).toEqual([{ lastMessageId: 5 }]));
    await waitFor(() => expect(document.title).toBe('ARC'));
    expect(screen.queryByLabelText(/unread/)).not.toBeInTheDocument();
  });

  it('refetches the list and the open thread after the socket reconnects', async () => {
    const waits: number[] = [];
    vi.spyOn(reconnectClock, 'cancel').mockImplementation(() => {});
    vi.spyOn(reconnectClock, 'delay').mockImplementation((ms, fn) => {
      waits.push(ms);
      queueMicrotask(fn);
      return 1 as unknown as ReturnType<typeof setTimeout>;
    });
    let chatLoads = 0;
    let messageLoads = 0;
    server.use(
      http.get('/api/chats', () => {
        chatLoads += 1;
        return HttpResponse.json([chat(5, 'Grace Hopper', 0, 'earlier')]);
      }),
      http.get('/api/chats/:id/messages', () => {
        messageLoads += 1;
        return HttpResponse.json({ messages: [line(3, 'earlier')] });
      }),
    );
    renderAt('/messenger/5');
    expect(await within(await screen.findByRole('log', { name: 'Messages' })).findByText('earlier')).toBeInTheDocument();
    await waitFor(() => expect(clients.length).toBeGreaterThan(0));
    expect(chatLoads).toBe(1);
    expect(messageLoads).toBe(1);

    act(() => clients.at(-1)?.close());
    await waitFor(() => expect(waits).toEqual([1000]));
    await waitFor(() => expect(chatLoads).toBe(2));
    expect(messageLoads).toBe(2);

    act(() => clients.at(-1)?.close());
    await waitFor(() => expect(waits).toEqual([1000, 1000]));
    await waitFor(() => expect(chatLoads).toBe(3));
    expect(messageLoads).toBe(3);
  });

  it('does not fetch messages for an id that is not a chat', async () => {
    let messageLoads = 0;
    server.use(
      http.get('/api/chats/:id/messages', () => {
        messageLoads += 1;
        return HttpResponse.json({ messages: [] });
      }),
    );
    renderAt('/messenger/nope');
    expect(await screen.findByText('Chat not found.')).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Messenger' })).toBeInTheDocument();
    expect(messageLoads).toBe(0);
  });

  it('shows a chat error and a list error', async () => {
    server.use(
      http.get('/api/chats', () => HttpResponse.json({ code: 'internal', message: 'Try later' }, { status: 500 })),
    );
    renderAt('/messenger');
    expect(await screen.findByText('Try later')).toBeInTheDocument();
  });

  it('shows the server message when a thread cannot be loaded', async () => {
    server.use(
      http.get('/api/chats/:id/messages', () =>
        HttpResponse.json({ code: 'not_found', message: 'Chat not found.' }, { status: 404 }),
      ),
    );
    renderAt('/messenger/5');
    expect(await screen.findByRole('heading', { name: 'Chat' })).toBeInTheDocument();
    expect(await screen.findByText('Chat not found.')).toBeInTheDocument();
  });

  it('puts the total unread count in the tab title and restores the previous title', async () => {
    document.title = 'ARC — Async Random Coffee';
    let release: (response: Response) => void = () => {};
    const pending = new Promise<Response>((resolve) => {
      release = resolve;
    });
    server.use(http.get('/api/chats', () => pending));
    const view = renderAt('/messenger');

    expect(await screen.findByRole('heading', { name: 'Messenger' })).toBeInTheDocument();
    expect(document.title).toBe('ARC — Async Random Coffee');

    await act(async () => {
      release(HttpResponse.json([chat(5, 'Grace Hopper', 1, 'a'), chat(8, 'Alan Turing', 2, 'b')]));
    });
    await waitFor(() => expect(document.title).toBe('(3) ARC'));

    await waitFor(() => expect(clients.length).toBeGreaterThan(0));
    act(() => {
      push({
        type: 'message.new',
        data: { chatId: 8, message: line(9, 'another', 4) },
      });
    });
    await waitFor(() => expect(document.title).toBe('(4) ARC'));

    view.unmount();
    expect(document.title).toBe('ARC — Async Random Coffee');
  });

  it('sets the tab title to ARC when every chat is read, including the open thread', async () => {
    document.title = 'ARC — Async Random Coffee';
    server.use(
      http.get('/api/chats', () => HttpResponse.json([chat(5, 'Grace Hopper', 2, 'earlier'), chat(8, 'Alan Turing', 0, 'hey')])),
      http.get('/api/chats/:id/messages', () => HttpResponse.json({ messages: [line(3, 'earlier')] })),
    );
    renderAt('/messenger/5');
    await waitFor(() => expect(document.title).toBe('ARC'));
  });

  it('does not offer notifications when the browser has none', async () => {
    renderAt('/messenger');
    expect(await screen.findByRole('heading', { name: 'Messenger' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Enable notifications' })).not.toBeInTheDocument();
  });

  it('requests notification permission only from the enable button, then hides it', async () => {
    const user = userEvent.setup();
    const denied = installNotification('default', 'denied');
    const first = renderAt('/messenger');
    const button = await screen.findByRole('button', { name: 'Enable notifications' });
    expect(denied.requestPermission).not.toHaveBeenCalled();

    await user.click(button);
    await waitFor(() => expect(denied.requestPermission).toHaveBeenCalledTimes(1));
    expect(screen.queryByRole('button', { name: 'Enable notifications' })).not.toBeInTheDocument();
    first.unmount();

    const granted = installNotification('default', 'granted');
    renderAt('/messenger');
    await user.click(await screen.findByRole('button', { name: 'Enable notifications' }));
    await waitFor(() => expect(granted.requestPermission).toHaveBeenCalledTimes(1));
    expect(screen.queryByRole('button', { name: 'Enable notifications' })).not.toBeInTheDocument();
  });

  it('hides Enable notifications when permission is already granted or denied', async () => {
    installNotification('granted');
    const granted = renderAt('/messenger');
    expect(await screen.findByRole('heading', { name: 'Messenger' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Enable notifications' })).not.toBeInTheDocument();
    granted.unmount();

    const { requestPermission } = installNotification('denied');
    renderAt('/messenger');
    expect(await screen.findByRole('heading', { name: 'Messenger' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Enable notifications' })).not.toBeInTheDocument();
    expect(requestPermission).not.toHaveBeenCalled();
  });

  it('notifies with the partner name and the first 80 characters while the tab is hidden', async () => {
    const { posted, requestPermission } = installNotification('granted');
    setDocumentHidden(true);
    const long = `${'a'.repeat(79)}😀tail`;
    server.use(http.get('/api/chats', () => HttpResponse.json([chat(5, 'Grace Hopper', 0, 'earlier')])));
    renderAt('/messenger');
    expect(await findChatRow(/Grace Hopper/)).toBeInTheDocument();
    await waitFor(() => expect(clients.length).toBeGreaterThan(0));
    expect(requestPermission).not.toHaveBeenCalled();

    act(() => {
      push({ type: 'message.new', data: { chatId: 5, message: line(4, 'hi', 3) } });
    });
    await waitFor(() => expect(posted).toHaveLength(1));
    expect(posted[0]?.title).toBe('Grace Hopper');
    expect(posted[0]?.body).toBe('hi');

    act(() => {
      push({ type: 'message.new', data: { chatId: 5, message: line(5, long, 3) } });
    });
    await waitFor(() => expect(posted[1]?.body).toBe(`${'a'.repeat(79)}😀`));

    act(() => {
      push({ type: 'message.new', data: { chatId: 99, message: line(6, 'missing chat', 3) } });
      push({ type: 'chat.new', data: chat(11, 'Linus Torvalds', 0, 'y'.repeat(90)) });
      push({ type: 'chat.new', data: chat(12, 'Katherine Johnson', 0, null) });
    });
    await waitFor(() => expect(posted.map((item) => item.title)).toContain('Katherine Johnson'));
    expect(posted.map((item) => item.body)).not.toContain('missing chat');
    expect(posted[2]?.title).toBe('Linus Torvalds');
    expect(posted[2]?.body).toBe('y'.repeat(80));
    expect(posted[3]?.title).toBe('Katherine Johnson');
    expect(posted[3]?.body).toBe('');

    setDocumentHidden(false);
    act(() => {
      push({ type: 'message.new', data: { chatId: 5, message: line(7, 'visible now', 3) } });
      push({ type: 'chat.new', data: chat(13, 'Edsger Dijkstra', 0, 'nope') });
    });
    expect(await screen.findByText('visible now')).toBeInTheDocument();
    expect(await findChatRow(/Edsger Dijkstra/)).toBeInTheDocument();
    expect(posted).toHaveLength(4);
    expect(requestPermission).not.toHaveBeenCalled();

    const note = posted[2];
    const open = note?.onclick;
    if (!note || !open) throw new Error('missing notification');
    const focus = vi.spyOn(window, 'focus').mockImplementation(() => {});
    act(() => {
      open(new Event('click'));
    });
    expect(focus).toHaveBeenCalled();
    expect(note.close).toHaveBeenCalled();
    expect(await screen.findByRole('heading', { name: 'Linus Torvalds' })).toBeInTheDocument();
    expect(screen.getByTestId('location')).toHaveTextContent('/messenger/11');
  });

  it('does not notify or ask while permission is still the default', async () => {
    const { posted, requestPermission } = installNotification('default');
    setDocumentHidden(true);
    server.use(http.get('/api/chats', () => HttpResponse.json([chat(5, 'Grace Hopper', 0, 'earlier')])));
    renderAt('/messenger');
    expect(await screen.findByRole('button', { name: 'Enable notifications' })).toBeInTheDocument();
    await waitFor(() => expect(clients.length).toBeGreaterThan(0));

    act(() => {
      push({ type: 'message.new', data: { chatId: 5, message: line(4, 'hi', 3) } });
      push({ type: 'chat.new', data: chat(11, 'Linus Torvalds', 0, 'hey') });
    });
    expect(await screen.findByText('hi')).toBeInTheDocument();
    expect(await findChatRow(/Linus Torvalds/)).toBeInTheDocument();
    expect(posted).toHaveLength(0);
    expect(requestPermission).not.toHaveBeenCalled();
  });
});
