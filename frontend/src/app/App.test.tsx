import { QueryClient } from '@tanstack/react-query';
import { act, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import type { ReactElement } from 'react';
import { MemoryRouter, useLocation } from 'react-router';
import { beforeEach, describe, expect, it } from 'vitest';
import { meKey } from '../api/me';
import { admin, member, profileFor } from '../test/fixtures';
import { render } from '../test/render';
import { chatSocket, server } from '../test/server';
import { App } from './App';
import { AppRoutes } from './routes';

function LocationEcho() {
  const location = useLocation();
  return <div data-testid="location">{`${location.pathname}${location.search}`}</div>;
}

function renderAt(path: string, ui: ReactElement = <AppRoutes />, queryClient?: QueryClient) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <LocationEcho />
      {ui}
    </MemoryRouter>,
    { queryClient },
  );
}

describe('app shell', () => {
  beforeEach(() => {
    server.use(
      http.get('/api/me', () => HttpResponse.json(member)),
      http.get('/api/users/:id', () => HttpResponse.json(profileFor(member))),
      http.get('/api/users/:id/wall', () => HttpResponse.json({ posts: [] })),
      http.get('/api/chats', () => HttpResponse.json([])),
      http.get('/api/chats/:id/messages', () => HttpResponse.json({ messages: [] })),
    );
  });

  it('sends a signed-out visitor to login and keeps the path', async () => {
    server.use(http.get('/api/me', () => new HttpResponse(null, { status: 401 })));
    renderAt('/messenger?x=1');

    expect(await screen.findByRole('heading', { name: 'Sign in' })).toBeInTheDocument();
    expect(screen.getByTestId('location')).toHaveTextContent('/login?next=%2Fmessenger%3Fx%3D1');
    expect(screen.getByText('Meet your teammates when you have time')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Sign in with Google' })).toHaveAttribute(
      'href',
      '/auth/google/start?next=%2Fmessenger%3Fx%3D1',
    );
    expect(screen.queryByRole('link', { name: 'ARC' })).not.toBeInTheDocument();
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('shows a loading state until the session resolves', async () => {
    let release: (response: Response) => void = () => {};
    const pending = new Promise<Response>((resolve) => {
      release = resolve;
    });
    server.use(http.get('/api/me', () => pending));
    renderAt('/messenger');

    expect(screen.getByLabelText('Loading')).toBeInTheDocument();
    release(HttpResponse.json(member));
    expect(await screen.findByRole('heading', { name: 'Messenger' })).toBeInTheDocument();
  });

  it('hides Admin from a regular member and opens their own profile', async () => {
    renderAt('/');

    expect(await screen.findByRole('heading', { name: 'Ada Lovelace' })).toBeInTheDocument();
    expect(screen.getByTestId('location')).toHaveTextContent('/u/7');
    expect(screen.getByRole('link', { name: 'ARC' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'My profile' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Messenger' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Log out' })).toBeInTheDocument();
    expect(screen.getByRole('search', { name: 'Search' })).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Admin' })).not.toBeInTheDocument();
    expect(screen.getByRole('banner')).toHaveStyle({ backgroundColor: '#3b5998', boxShadow: 'none' });
    expect(screen.getByTestId('content')).toHaveStyle({ maxWidth: '980px' });
  });

  it('shows Admin only when me.isAdmin is true', async () => {
    server.use(
      http.get('/api/me', () => HttpResponse.json(admin)),
      http.get('/api/admin/stats', () =>
        HttpResponse.json({ users: 1, availableUsers: 1, chats: 0, messages: 0, wallPosts: 0, photos: 0 }),
      ),
    );
    renderAt('/admin');

    expect(await screen.findByRole('link', { name: 'Admin' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Admin' })).toBeInTheDocument();
  });

  it('shows not found to a non-admin at /admin without redirecting', async () => {
    renderAt('/admin');
    expect(await screen.findByRole('heading', { name: '404 · Page not found' })).toBeInTheDocument();
    expect(screen.getByTestId('location').textContent).toBe('/admin');
    expect(screen.queryByRole('region', { name: 'Stats' })).not.toBeInTheDocument();
    expect(screen.queryByRole('region', { name: 'Remove profile' })).not.toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: 'Admin' })).not.toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Admin' })).not.toBeInTheDocument();
  });

  it('shows unread messages in the top bar and the tab title on any page, and keeps them live', async () => {
    const clients: { send: (data: string) => void }[] = [];
    const previousTitle = document.title;
    server.use(
      chatSocket.addEventListener('connection', ({ client }) => {
        clients.push(client);
      }),
      http.get('/api/chats', () =>
        HttpResponse.json([
          {
            id: 5,
            partner: { id: 105, name: 'Grace Hopper', avatarUrl: null },
            lastMessage: { body: 'hi', kind: 'text', createdAt: '2026-11-11T12:00:00Z' },
            unreadCount: 2,
          },
        ]),
      ),
    );
    const view = renderAt('/u/7');
    expect(await screen.findByRole('heading', { name: 'Ada Lovelace' })).toBeInTheDocument();

    const banner = screen.getByRole('banner');
    expect(await within(banner).findByLabelText('2 unread messages')).toHaveTextContent('2');
    // The badge sits next to the link, so the link keeps its plain name.
    expect(within(banner).getByRole('link', { name: 'Messenger' })).toBeInTheDocument();
    await waitFor(() => expect(document.title).toBe('(2) ARC'));

    await waitFor(() => expect(clients.length).toBeGreaterThan(0));
    act(() => {
      clients.at(-1)?.send(
        JSON.stringify({
          type: 'message.new',
          data: { chatId: 5, message: { id: 9, senderId: 105, kind: 'text', body: 'still there?', createdAt: '2026-11-11T12:01:00Z' } },
        }),
      );
    });
    expect(await within(banner).findByLabelText('3 unread messages')).toBeInTheDocument();
    await waitFor(() => expect(document.title).toBe('(3) ARC'));

    view.unmount();
    expect(document.title).toBe(previousTitle);
  });

  it('shows no unread badge when everything is read', async () => {
    renderAt('/u/7');
    expect(await screen.findByRole('heading', { name: 'Ada Lovelace' })).toBeInTheDocument();
    await waitFor(() => expect(document.title).toBe('ARC'));
    expect(screen.queryByLabelText(/unread/)).not.toBeInTheDocument();
    document.title = '';
  });

  it('opens messenger from the top bar', async () => {
    const user = userEvent.setup();
    renderAt('/u/7');
    expect(await screen.findByRole('heading', { name: 'Ada Lovelace' })).toBeInTheDocument();
    await user.click(screen.getByRole('link', { name: 'Messenger' }));
    expect(await screen.findByRole('heading', { name: 'Messenger' })).toBeInTheDocument();
    expect(screen.getByTestId('location')).toHaveTextContent('/messenger');
  });

  it('opens a chat', async () => {
    renderAt('/messenger/42');
    expect(await screen.findByRole('heading', { name: 'Chat' })).toBeInTheDocument();
    expect(await screen.findByRole('textbox', { name: 'Message' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Messenger' })).toBeInTheDocument();
    expect(screen.getByTestId('location')).toHaveTextContent('/messenger/42');
  });

  it.each([member, admin])('shows not found for an unknown path for $name', async (viewer) => {
    server.use(http.get('/api/me', () => HttpResponse.json(viewer)));
    renderAt('/nope');
    expect(await screen.findByRole('heading', { name: '404 · Page not found' })).toBeInTheDocument();
    expect(screen.getByTestId('location').textContent).toBe('/nope');
    expect(screen.getByText('We couldn’t find that page.')).toBeInTheDocument();
  });

  it('logs out, clears the cache, and opens login', async () => {
    const user = userEvent.setup();
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
    let logoutCalls = 0;
    server.use(
      http.post('/auth/logout', () => {
        logoutCalls += 1;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    renderAt('/u/7', <AppRoutes />, queryClient);

    expect(await screen.findByRole('button', { name: 'Log out' })).toBeInTheDocument();
    expect(queryClient.getQueryData(meKey)).toEqual(member);
    await user.click(screen.getByRole('button', { name: 'Log out' }));

    expect(await screen.findByRole('heading', { name: 'Sign in' })).toBeInTheDocument();
    expect(screen.getByTestId('location').textContent).toBe('/login');
    expect(screen.queryByRole('link', { name: 'Messenger' })).not.toBeInTheDocument();
    expect(logoutCalls).toBe(1);
    expect(queryClient.getQueryCache().getAll()).toEqual([]);
  });

  it('stays signed in when logout fails', async () => {
    const user = userEvent.setup();
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
    server.use(
      http.post('/auth/logout', () =>
        HttpResponse.json({ code: 'internal', message: 'Something went wrong.' }, { status: 500 }),
      ),
    );
    renderAt('/u/7', <AppRoutes />, queryClient);

    await user.click(await screen.findByRole('button', { name: 'Log out' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('Something went wrong.');
    expect(screen.getByTestId('location').textContent).toBe('/u/7');
    expect(screen.getByRole('heading', { name: 'Ada Lovelace' })).toBeInTheDocument();
    expect(queryClient.getQueryData(meKey)).toEqual(member);
  });

  it('shows login without asking for a session', async () => {
    server.use(http.get('/api/me', () => HttpResponse.json({ code: 'unauthenticated' }, { status: 401 })));
    renderAt('/login?next=%2Fmessenger');
    expect(await screen.findByRole('heading', { name: 'Sign in' })).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Messenger' })).not.toBeInTheDocument();
  });

  it('notifies when the session request fails for another reason', async () => {
    server.use(
      http.get('/api/me', () => HttpResponse.json({ code: 'unavailable', message: 'Try again later' }, { status: 503 })),
    );
    renderAt('/messenger');
    expect(await screen.findByRole('heading', { name: 'Sign in' })).toBeInTheDocument();
    expect(await screen.findByRole('alert')).toHaveTextContent('Try again later');
  });

  it('mounts through App', async () => {
    renderAt('/', <App />);
    expect(await screen.findByRole('heading', { name: 'Ada Lovelace' })).toBeInTheDocument();
  });
});
