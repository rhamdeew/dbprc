import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router';
import { beforeEach, describe, expect, it } from 'vitest';
import type { AdminPreview, AdminStats } from '../api/admin';
import { admin, member } from '../test/fixtures';
import { render } from '../test/render';
import { server } from '../test/server';
import { AdminPage } from './AdminPage';

const stats: AdminStats = {
  users: 12,
  availableUsers: 7,
  chats: 4,
  messages: 30,
  wallPosts: 9,
  photos: 5,
};

const preview: AdminPreview = {
  exists: true,
  wallPosts: 2,
  authoredPosts: 3,
  photos: 1,
  chats: 1,
  messages: 4,
};

const emptyPreview: AdminPreview = {
  exists: false,
  wallPosts: 0,
  authoredPosts: 0,
  photos: 0,
  chats: 0,
  messages: 0,
};

function LocationEcho() {
  const location = useLocation();
  return <div data-testid="location">{location.pathname}</div>;
}

function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/admin']}>
      <LocationEcho />
      <Routes>
        <Route path="/admin" element={<AdminPage />} />
        <Route path="/" element={<h1>Home</h1>} />
      </Routes>
    </MemoryRouter>,
  );
}

function statsRegion() {
  return within(screen.getByRole('region', { name: 'Stats' }));
}

function removeRegion() {
  return within(screen.getByRole('region', { name: 'Remove profile' }));
}

describe('admin page', () => {
  beforeEach(() => {
    server.use(
      http.get('/api/me', () => HttpResponse.json(admin)),
      http.get('/api/admin/stats', () => HttpResponse.json(stats)),
    );
  });

  it('renders each stat from /api/admin/stats', async () => {
    renderPage();

    expect(await screen.findByRole('heading', { name: 'Admin' })).toBeInTheDocument();
    const region = within(await screen.findByRole('region', { name: 'Stats' }));
    expect(within(await region.findByRole('group', { name: 'Users' })).getByText('12')).toBeInTheDocument();
    expect(within(region.getByRole('group', { name: 'Available for chat' })).getByText('7')).toBeInTheDocument();
    expect(within(region.getByRole('group', { name: 'Chats' })).getByText('4')).toBeInTheDocument();
    expect(within(region.getByRole('group', { name: 'Messages' })).getByText('30')).toBeInTheDocument();
    expect(within(region.getByRole('group', { name: 'Wall posts' })).getByText('9')).toBeInTheDocument();
    expect(within(region.getByRole('group', { name: 'Photos' })).getByText('5')).toBeInTheDocument();
    expect(screen.queryByRole('list')).not.toBeInTheDocument();
  });

  it('shows a loader, then the API error when stats fail', async () => {
    let release: (response: Response) => void = () => {};
    const pending = new Promise<Response>((resolve) => {
      release = resolve;
    });
    server.use(http.get('/api/admin/stats', () => pending));
    renderPage();

    expect(await screen.findByLabelText('Loading stats')).toBeInTheDocument();
    release(HttpResponse.json({ code: 'internal', message: 'Stats are down.' }, { status: 500 }));

    expect(await screen.findByRole('alert')).toHaveTextContent('Stats are down.');
    expect(await screen.findByText('Stats are down.', { selector: 'p' })).toBeInTheDocument();
    expect(screen.queryByRole('group', { name: 'Users' })).not.toBeInTheDocument();
  });

  it('previews counts for an email and hides them when the email changes', async () => {
    const user = userEvent.setup();
    const requested: string[] = [];
    server.use(
      http.get('/api/admin/users/preview', ({ request }) => {
        requested.push(new URL(request.url).searchParams.get('email') ?? '');
        return HttpResponse.json(preview);
      }),
    );
    renderPage();

    expect(await screen.findByRole('button', { name: 'Preview' })).toBeDisabled();
    await user.type(screen.getByRole('textbox', { name: 'Email' }), '  ada@corp.test  ');
    await user.click(screen.getByRole('button', { name: 'Preview' }));

    const counts = await removeRegion().findByRole('group', { name: 'Posts on their wall' });
    expect(within(counts).getByText('2')).toBeInTheDocument();
    expect(within(removeRegion().getByRole('group', { name: 'Posts they wrote' })).getByText('3')).toBeInTheDocument();
    expect(within(removeRegion().getByRole('group', { name: 'Photos' })).getByText('1')).toBeInTheDocument();
    expect(within(removeRegion().getByRole('group', { name: 'Chats' })).getByText('1')).toBeInTheDocument();
    expect(within(removeRegion().getByRole('group', { name: 'Messages' })).getByText('4')).toBeInTheDocument();
    expect(requested).toEqual(['ada@corp.test']);
    expect(removeRegion().getByRole('button', { name: 'Remove profile' })).toBeInTheDocument();

    await user.type(screen.getByRole('textbox', { name: 'Email' }), 'x');
    expect(screen.queryByRole('group', { name: 'Posts on their wall' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Remove profile' })).not.toBeInTheDocument();
  });

  it('shows zero counts and no delete when the email has no profile', async () => {
    const user = userEvent.setup();
    server.use(http.get('/api/admin/users/preview', () => HttpResponse.json(emptyPreview)));
    renderPage();

    await user.type(await screen.findByRole('textbox', { name: 'Email' }), 'nobody@corp.test');
    await user.click(screen.getByRole('button', { name: 'Preview' }));

    expect(await screen.findByText('No profile with that email.')).toBeInTheDocument();
    expect(within(removeRegion().getByRole('group', { name: 'Posts on their wall' })).getByText('0')).toBeInTheDocument();
    expect(within(removeRegion().getByRole('group', { name: 'Posts they wrote' })).getByText('0')).toBeInTheDocument();
    expect(within(removeRegion().getByRole('group', { name: 'Photos' })).getByText('0')).toBeInTheDocument();
    expect(within(removeRegion().getByRole('group', { name: 'Chats' })).getByText('0')).toBeInTheDocument();
    expect(within(removeRegion().getByRole('group', { name: 'Messages' })).getByText('0')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Remove profile' })).not.toBeInTheDocument();
  });

  it('blocks delete until the confirmation email matches', async () => {
    const user = userEvent.setup();
    const deleted: unknown[] = [];
    server.use(
      http.get('/api/admin/users/preview', () => HttpResponse.json(preview)),
      http.delete('/api/admin/users', async ({ request }) => {
        deleted.push(await request.json());
        return new HttpResponse(null, { status: 204 });
      }),
    );
    renderPage();

    await user.type(await screen.findByRole('textbox', { name: 'Email' }), 'ada@corp.test');
    await user.click(screen.getByRole('button', { name: 'Preview' }));
    await user.click(await screen.findByRole('button', { name: 'Remove profile' }));

    const dialog = await screen.findByRole('dialog', { name: 'Remove this profile?' });
    const confirm = within(dialog).getByRole('textbox', { name: 'Confirm email' });
    const remove = within(dialog).getByRole('button', { name: 'Delete' });
    expect(remove).toBeDisabled();

    await user.type(confirm, 'Ada@corp.test');
    expect(remove).toBeDisabled();
    expect(within(dialog).getByText('Email does not match.')).toBeInTheDocument();
    expect(deleted).toEqual([]);

    await user.clear(confirm);
    await user.type(confirm, 'ada@corp.test');
    expect(within(dialog).queryByText('Email does not match.')).not.toBeInTheDocument();
    expect(remove).toBeEnabled();
  });

  it('removes the profile, notifies, and refreshes stats', async () => {
    const user = userEvent.setup();
    const deleted: unknown[] = [];
    let removed = false;
    const after: AdminStats = { ...stats, users: 11, wallPosts: 7, photos: 4, chats: 3, messages: 26 };
    server.use(
      http.get('/api/admin/stats', () => HttpResponse.json(removed ? after : stats)),
      http.get('/api/admin/users/preview', () => HttpResponse.json(preview)),
      http.delete('/api/admin/users', async ({ request }) => {
        deleted.push(await request.json());
        removed = true;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    renderPage();

    expect(within(await screen.findByRole('group', { name: 'Users' })).getByText('12')).toBeInTheDocument();
    await user.type(screen.getByRole('textbox', { name: 'Email' }), 'ada@corp.test');
    await user.click(screen.getByRole('button', { name: 'Preview' }));
    await user.click(await screen.findByRole('button', { name: 'Remove profile' }));

    const dialog = await screen.findByRole('dialog', { name: 'Remove this profile?' });
    await user.type(within(dialog).getByRole('textbox', { name: 'Confirm email' }), '  ada@corp.test  ');
    await user.click(within(dialog).getByRole('button', { name: 'Delete' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('Profile removed.');
    await waitFor(() => {
      expect(within(screen.getByRole('group', { name: 'Users' })).getByText('11')).toBeInTheDocument();
    });
    expect(within(statsRegion().getByRole('group', { name: 'Wall posts' })).getByText('7')).toBeInTheDocument();
    expect(within(statsRegion().getByRole('group', { name: 'Photos' })).getByText('4')).toBeInTheDocument();
    expect(within(statsRegion().getByRole('group', { name: 'Chats' })).getByText('3')).toBeInTheDocument();
    expect(within(statsRegion().getByRole('group', { name: 'Messages' })).getByText('26')).toBeInTheDocument();
    expect(deleted).toEqual([{ email: 'ada@corp.test' }]);
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    expect(screen.queryByText('Posts on their wall')).not.toBeInTheDocument();
    expect(screen.getByRole('textbox', { name: 'Email' })).toHaveValue('');
  });

  it('sends a non-admin to the home route', async () => {
    server.use(http.get('/api/me', () => HttpResponse.json(member)));
    renderPage();

    expect(await screen.findByRole('heading', { name: 'Home' })).toBeInTheDocument();
    expect(screen.getByTestId('location')).toHaveTextContent('/');
    expect(screen.queryByRole('heading', { name: 'Admin' })).not.toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: 'Remove profile' })).not.toBeInTheDocument();
  });
});
