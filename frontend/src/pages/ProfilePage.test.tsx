import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { delay, http, HttpResponse } from 'msw';
import { MemoryRouter, Route, Routes } from 'react-router';
import { describe, expect, it } from 'vitest';
import type { Interests, Profile } from '../api/profile';
import { member, profileFor } from '../test/fixtures';
import { render } from '../test/render';
import { server } from '../test/server';
import { ProfilePage } from './ProfilePage';

const colleague = profileFor(
  { ...member, id: 9, name: 'Alan Turing', email: 'alan@corp.test' },
  {
    isMe: false,
    interests: { hobby: 'chess', music: 'jazz', movies: '', other: '' },
    photos: [{ id: 3, url: '/uploads/photos/3.png' }],
  },
);

function renderProfile(id: string) {
  return render(
    <MemoryRouter initialEntries={[`/u/${id}`]}>
      <Routes>
        <Route path="/u/:id" element={<ProfilePage />} />
      </Routes>
    </MemoryRouter>,
  );
}

function mockProfile(profile: Profile) {
  server.use(
    http.get('/api/users/:id', () => HttpResponse.json(profile)),
    http.get('/api/users/:id/wall', () => HttpResponse.json({ posts: [] })),
  );
}

function imageFile(name = 'pic.png') {
  return new File(['png'], name, { type: 'image/png' });
}

function fileInput(name: string): HTMLInputElement {
  const input = document.querySelector(`input[aria-label="${name}"]`);
  if (!(input instanceof HTMLInputElement)) throw new Error(`No file input named ${name}`);
  return input;
}

describe('profile page', () => {
  it('shows own-profile controls and no way to start a chat', async () => {
    mockProfile(
      profileFor(member, {
        interests: { hobby: 'chess', music: '', movies: '', other: '' },
        photos: [{ id: 3, url: '/uploads/photos/3.png' }],
      }),
    );
    renderProfile(String(member.id));

    expect(await screen.findByRole('heading', { name: 'Ada Lovelace' })).toBeInTheDocument();
    expect(screen.getByText(member.email)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Change photo' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Edit Hobby' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Edit Music' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Add photo' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Delete photo 3' })).toHaveTextContent('×');
    expect(screen.getByRole('heading', { name: 'Photos (1/5)' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Information' })).toBeInTheDocument();
    expect(screen.getByText('chess')).toBeInTheDocument();
    expect(screen.getAllByText('Not set').length).toBeGreaterThan(0);
    expect(screen.getByRole('heading', { name: 'Wall' })).toBeInTheDocument();
    expect(await screen.findByText('No posts yet.')).toBeInTheDocument();
    expect(screen.getByRole('textbox', { name: 'Write on this wall' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /chat/i })).not.toBeInTheDocument();
    expect(screen.queryByRole('link', { name: /chat/i })).not.toBeInTheDocument();
    expect(screen.queryByText(/start chat/i)).not.toBeInTheDocument();
  });

  it('hides edit, upload, and delete controls on someone else’s profile', async () => {
    mockProfile(colleague);
    renderProfile('9');

    expect(await screen.findByRole('heading', { name: 'Alan Turing' })).toBeInTheDocument();
    expect(screen.getByText('chess')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Open photo 1' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Change photo' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /edit/i })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Add photo' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /delete photo/i })).not.toBeInTheDocument();
    expect(screen.queryByText(/start chat/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/not available for random chats/)).not.toBeInTheDocument();
  });

  it('says when a colleague is out of the random-chat pool', async () => {
    mockProfile({ ...colleague, available: false });
    renderProfile('9');

    expect(await screen.findByRole('heading', { name: 'Alan Turing' })).toBeInTheDocument();
    expect(screen.getByText(`${colleague.email} · not available for random chats`)).toBeInTheDocument();
  });

  it('saves one interest and keeps the other fields', async () => {
    const user = userEvent.setup();
    mockProfile(profileFor(member, { interests: { hobby: 'chess', music: 'jazz', movies: 'noir', other: 'walks' } }));
    let saved: Interests | undefined;
    server.use(
      http.put('/api/me/interests', async ({ request }) => {
        const body = (await request.json()) as Interests;
        saved = body;
        return HttpResponse.json(body);
      }),
    );
    renderProfile('7');

    await user.click(await screen.findByRole('button', { name: 'Edit Music' }));
    const music = screen.getByRole('textbox', { name: 'Music' });
    await user.clear(music);
    await user.type(music, '  bossa  ');
    await user.click(screen.getByRole('button', { name: 'Save' }));

    await waitFor(() =>
      expect(saved).toEqual({ hobby: 'chess', music: 'bossa', movies: 'noir', other: 'walks' }),
    );
    expect(await screen.findByText('bossa')).toBeInTheDocument();
    expect(screen.getByText('chess')).toBeInTheDocument();
    expect(screen.queryByRole('textbox', { name: 'Music' })).not.toBeInTheDocument();
  });

  it.each([
    ['500 emoji', '😀'.repeat(500), '😀'.repeat(500)],
    ['501 emoji', `${'😀'.repeat(500)}😁`, '😀'.repeat(500)],
    ['mixed characters at the boundary', `${'A'.repeat(499)}😀😁`, `${'A'.repeat(499)}😀`],
  ])('pastes and saves %s by Unicode code point', async (_label, pasted, expected) => {
    const user = userEvent.setup();
    mockProfile(profileFor(member));
    let saved: Interests | undefined;
    server.use(http.put('/api/me/interests', async ({ request }) => {
      saved = (await request.json()) as Interests;
      return HttpResponse.json(saved);
    }));
    renderProfile('7');

    await user.click(await screen.findByRole('button', { name: 'Edit Hobby' }));
    const input = screen.getByRole('textbox', { name: 'Hobby' });
    await user.click(input);
    await user.paste(pasted);
    expect(input).toHaveValue(expected);
    await user.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(saved?.hobby).toBe(expected));
    expect(await screen.findByText(expected)).toBeInTheDocument();
  });

  it('cancels an interest edit without saving', async () => {
    const user = userEvent.setup();
    mockProfile(profileFor(member, { interests: { hobby: 'chess', music: '', movies: '', other: '' } }));
    let saved = 0;
    server.use(
      http.put('/api/me/interests', () => {
        saved += 1;
        return HttpResponse.json({ hobby: 'nope', music: '', movies: '', other: '' });
      }),
    );
    renderProfile('7');

    await user.click(await screen.findByRole('button', { name: 'Edit Hobby' }));
    await user.type(screen.getByRole('textbox', { name: 'Hobby' }), 'nope');
    await user.click(screen.getByRole('button', { name: 'Cancel' }));

    expect(screen.queryByRole('textbox', { name: 'Hobby' })).not.toBeInTheDocument();
    expect(screen.getByText('chess')).toBeInTheDocument();
    expect(saved).toBe(0);
  });

  it('keeps the editor open when saving interests fails', async () => {
    const user = userEvent.setup();
    mockProfile(profileFor(member));
    server.use(
      http.put('/api/me/interests', () =>
        HttpResponse.json({ code: 'too_long', message: 'Hobby must be 500 characters or fewer.' }, { status: 400 }),
      ),
    );
    renderProfile('7');

    await user.click(await screen.findByRole('button', { name: 'Edit Hobby' }));
    await user.type(screen.getByRole('textbox', { name: 'Hobby' }), 'chess');
    await user.click(screen.getByRole('button', { name: 'Save' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('Hobby must be 500 characters or fewer.');
    expect(screen.getByRole('textbox', { name: 'Hobby' })).toHaveValue('chess');
  });

  it('hides Add photo at the 5-photo limit', async () => {
    const photos = [1, 2, 3, 4, 5].map((id) => ({ id, url: `/uploads/photos/${id}.png` }));
    mockProfile(profileFor(member, { photos }));
    renderProfile('7');

    expect(await screen.findByRole('heading', { name: 'Photos (5/5)' })).toBeInTheDocument();
    expect(screen.getAllByRole('button', { name: /open photo/i })).toHaveLength(5);
    expect(screen.queryByRole('button', { name: 'Add photo' })).not.toBeInTheDocument();
    expect(document.querySelector('input[aria-label="Add a photo"]')).toBeNull();
  });

  it('opens the lightbox on click and moves with the keyboard', async () => {
    const user = userEvent.setup();
    mockProfile(
      profileFor(member, {
        photos: [
          { id: 3, url: '/uploads/photos/3.png' },
          { id: 4, url: '/uploads/photos/4.png' },
        ],
      }),
    );
    renderProfile('7');

    await user.click(await screen.findByRole('button', { name: 'Open photo 1' }));

    expect(await screen.findByRole('dialog', { name: 'Lightbox' })).toBeInTheDocument();
    expect(screen.getByRole('group', { name: '1 of 2' })).toBeInTheDocument();
    await user.keyboard('{ArrowRight}');
    expect(await screen.findByRole('group', { name: '2 of 2' })).toBeInTheDocument();
    expect(screen.getByRole('img', { name: 'Photo 2 of 2' })).toHaveAttribute('src', '/uploads/photos/4.png');
  });

  it('shows avatar upload progress and the new photo', async () => {
    const user = userEvent.setup();
    mockProfile(profileFor(member));
    server.use(
      http.post('/api/me/avatar', async () => {
        await delay(80);
        return HttpResponse.json({ avatarUrl: '/uploads/avatars/ada.png' });
      }),
    );
    renderProfile('7');

    expect(await screen.findByRole('button', { name: 'Change photo' })).toBeInTheDocument();
    await user.upload(fileInput('Profile photo'), imageFile());

    expect(await screen.findByRole('progressbar', { name: 'Avatar upload' })).toBeInTheDocument();
    expect(await screen.findByRole('img', { name: 'Ada Lovelace, profile photo' })).toHaveAttribute(
      'src',
      '/uploads/avatars/ada.png',
    );
    expect(screen.queryByRole('progressbar', { name: 'Avatar upload' })).not.toBeInTheDocument();
  });

  it('shows an upload error', async () => {
    const user = userEvent.setup();
    mockProfile(profileFor(member));
    server.use(
      http.post('/api/me/avatar', () =>
        HttpResponse.json(
          { code: 'unsupported_type', message: 'Use a JPEG, PNG, GIF, or WebP image.' },
          { status: 400 },
        ),
      ),
    );
    renderProfile('7');

    expect(await screen.findByRole('button', { name: 'Change photo' })).toBeInTheDocument();
    await user.upload(fileInput('Profile photo'), imageFile('notes.txt'));

    expect(await screen.findByRole('alert')).toHaveTextContent('Use a JPEG, PNG, GIF, or WebP image.');
    expect(screen.queryByRole('img', { name: 'Ada Lovelace, profile photo' })).not.toBeInTheDocument();
  });

  it('shows a friendly invalid-image error without adding a photo', async () => {
    const user = userEvent.setup();
    mockProfile(profileFor(member));
    server.use(
      http.post('/api/me/photos', () => HttpResponse.json(
        { code: 'invalid_image', message: 'That image could not be read. Try another image.' },
        { status: 400 },
      )),
    );
    renderProfile('7');

    expect(await screen.findByRole('button', { name: 'Add photo' })).toBeInTheDocument();
    await user.upload(fileInput('Add a photo'), imageFile('truncated.png'));

    expect(await screen.findByRole('alert')).toHaveTextContent('That image could not be read. Try another image.');
    expect(screen.getByRole('heading', { name: 'Photos (0/5)' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Open photo/ })).not.toBeInTheDocument();
    expect(screen.queryByRole('progressbar', { name: 'Photo upload' })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Add photo' })).toBeEnabled();
  });

  it('adds a photo and deletes it after confirm', async () => {
    const user = userEvent.setup();
    mockProfile(profileFor(member, { photos: [{ id: 3, url: '/uploads/photos/3.png' }] }));
    let deleted = 0;
    server.use(
      http.post('/api/me/photos', () => HttpResponse.json({ id: 8, url: '/uploads/photos/8.png' }, { status: 201 })),
      http.delete('/api/me/photos/:id', ({ params }) => {
        deleted += 1;
        expect(params.id).toBe('3');
        return new HttpResponse(null, { status: 204 });
      }),
    );
    renderProfile('7');

    expect(await screen.findByRole('button', { name: 'Add photo' })).toBeInTheDocument();
    await user.upload(fileInput('Add a photo'), imageFile());
    expect(await screen.findByRole('heading', { name: 'Photos (2/5)' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Open photo 2' })).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Delete photo 3' }));
    expect(await screen.findByRole('dialog', { name: 'Delete this photo?' })).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(deleted).toBe(0);
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Delete this photo?' })).not.toBeInTheDocument());

    await user.click(screen.getByRole('button', { name: 'Delete photo 3' }));
    await user.click(await screen.findByRole('button', { name: 'Delete' }));
    await waitFor(() => expect(deleted).toBe(1));
    expect(screen.queryByRole('button', { name: 'Delete photo 3' })).not.toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Photos (1/5)' })).toBeInTheDocument();
  });

  it('shows a loader, then a missing profile', async () => {
    let release: (response: Response) => void = () => {};
    const pending = new Promise<Response>((resolve) => {
      release = resolve;
    });
    server.use(http.get('/api/users/:id', () => pending));
    renderProfile('7');

    expect(screen.getByLabelText('Loading profile')).toBeInTheDocument();
    release(HttpResponse.json({ code: 'not_found', message: 'Profile not found.' }, { status: 404 }));

    expect(await screen.findByRole('heading', { name: 'Profile' })).toBeInTheDocument();
    expect(await screen.findByRole('alert')).toHaveTextContent('Profile not found.');
    expect(screen.getByText('Profile not found.', { selector: 'p' })).toBeInTheDocument();
  });

  it('does not request a profile for a bad id', async () => {
    renderProfile('nope');
    expect(await screen.findByRole('heading', { name: 'Profile' })).toBeInTheDocument();
    expect(screen.getByText('Profile not found.')).toBeInTheDocument();
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });
});
