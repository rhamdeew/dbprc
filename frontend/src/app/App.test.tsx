import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import type { ReactElement } from 'react';
import { MemoryRouter, useLocation } from 'react-router';
import { beforeEach, describe, expect, it } from 'vitest';
import { admin, member } from '../test/fixtures';
import { render } from '../test/render';
import { server } from '../test/server';
import { App } from './App';
import { AppRoutes } from './routes';

function LocationEcho() {
  const location = useLocation();
  return <div data-testid="location">{`${location.pathname}${location.search}`}</div>;
}

function renderAt(path: string, ui: ReactElement = <AppRoutes />) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <LocationEcho />
      {ui}
    </MemoryRouter>,
  );
}

describe('app shell', () => {
  beforeEach(() => {
    server.use(http.get('/api/me', () => HttpResponse.json(member)));
  });

  it('sends a signed-out visitor to login and keeps the path', async () => {
    server.use(http.get('/api/me', () => new HttpResponse(null, { status: 401 })));
    renderAt('/messenger?x=1');

    expect(await screen.findByRole('heading', { name: 'Sign in' })).toBeInTheDocument();
    expect(screen.getByTestId('location')).toHaveTextContent('/login?next=%2Fmessenger%3Fx%3D1');
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

    expect(await screen.findByRole('heading', { name: 'Profile' })).toBeInTheDocument();
    expect(screen.getByText('User 7')).toBeInTheDocument();
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
    server.use(http.get('/api/me', () => HttpResponse.json(admin)));
    renderAt('/admin');

    expect(await screen.findByRole('link', { name: 'Admin' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Admin' })).toBeInTheDocument();
  });

  it('sends a non-admin away from /admin', async () => {
    renderAt('/admin');
    expect(await screen.findByRole('heading', { name: 'Profile' })).toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: 'Admin' })).not.toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Admin' })).not.toBeInTheDocument();
  });

  it('opens messenger from the top bar', async () => {
    const user = userEvent.setup();
    renderAt('/u/7');
    expect(await screen.findByText('User 7')).toBeInTheDocument();
    await user.click(screen.getByRole('link', { name: 'Messenger' }));
    expect(await screen.findByRole('heading', { name: 'Messenger' })).toBeInTheDocument();
    expect(screen.getByTestId('location')).toHaveTextContent('/messenger');
  });

  it('opens a chat and sends unknown paths to the profile', async () => {
    renderAt('/messenger/42');
    expect(await screen.findByRole('heading', { name: 'Chat' })).toBeInTheDocument();
    expect(screen.getByText('Conversation 42')).toBeInTheDocument();
    expect(screen.getByTestId('location')).toHaveTextContent('/messenger/42');
  });

  it('sends an unknown path to the signed-in profile', async () => {
    renderAt('/nope');
    expect(await screen.findByRole('heading', { name: 'Profile' })).toBeInTheDocument();
    expect(screen.getByTestId('location')).toHaveTextContent('/u/7');
  });

  it('logs out to the login page', async () => {
    const user = userEvent.setup();
    renderAt('/u/7');
    expect(await screen.findByRole('button', { name: 'Log out' })).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Log out' }));
    expect(await screen.findByRole('heading', { name: 'Sign in' })).toBeInTheDocument();
    expect(screen.getByTestId('location')).toHaveTextContent('/login');
    expect(screen.queryByRole('link', { name: 'Messenger' })).not.toBeInTheDocument();
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
    expect(await screen.findByRole('heading', { name: 'Profile' })).toBeInTheDocument();
    expect(screen.getByText('User 7')).toBeInTheDocument();
  });
});
