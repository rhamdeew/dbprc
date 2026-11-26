import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { delay, http, HttpResponse } from 'msw';
import { MemoryRouter, Route, Routes } from 'react-router';
import { describe, expect, it } from 'vitest';
import type { PhotoComment } from '../api/photos';
import type { Interests, Profile, ProfilePhoto } from '../api/profile';
import { member, profileFor } from '../test/fixtures';
import { render } from '../test/render';
import { server } from '../test/server';
import { ProfilePage } from './ProfilePage';

function galleryPhoto(id: number, fields: Partial<ProfilePhoto> = {}): ProfilePhoto {
  return { id, url: `/uploads/photos/${id}.png`, likeCount: 0, likedByMe: false, commentCount: 0, ...fields };
}

const colleague = profileFor(
  { ...member, id: 9, name: 'Alan Turing', email: 'alan@corp.test' },
  {
    isMe: false,
    interests: { hobby: 'chess', music: 'jazz', movies: '', other: '' },
    photos: [galleryPhoto(3)],
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
    http.get('/api/photos/:id/comments', () => HttpResponse.json({ comments: [] })),
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
        photos: [galleryPhoto(3)],
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
    const photos = [1, 2, 3, 4, 5].map((id) => galleryPhoto(id));
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
          galleryPhoto(3),
          galleryPhoto(4),
        ],
      }),
    );
    server.use(http.get('/api/photos/:id/comments', ({ params }) => HttpResponse.json({
      comments: [galleryComment(Number(params.id), `Comment on photo ${params.id}`, false)],
    })));
    renderProfile('7');

    await user.click(await screen.findByRole('button', { name: 'Open photo 1' }));

    expect(await screen.findByRole('dialog', { name: 'Lightbox' })).toBeInTheDocument();
    expect(await screen.findByText('Comment on photo 3')).toBeInTheDocument();
    expect(screen.getByRole('group', { name: '1 of 2' })).toBeInTheDocument();
    await user.keyboard('{ArrowRight}');
    expect(await screen.findByRole('group', { name: '2 of 2' })).toBeInTheDocument();
    expect(screen.getByRole('img', { name: 'Photo 2 of 2' })).toHaveAttribute('src', '/uploads/photos/4.png');
    expect(await screen.findByText('Comment on photo 4')).toBeInTheDocument();
    expect(screen.queryByText('Comment on photo 3')).not.toBeInTheDocument();
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
    mockProfile(profileFor(member, { photos: [galleryPhoto(3)] }));
    let deleted = 0;
    server.use(
      http.post('/api/me/photos', () => HttpResponse.json(galleryPhoto(8), { status: 201 })),
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

describe('profile settings', () => {
  it.each([
    ['Nickname', 'nickname', 40],
    ['Where are you from?', 'hometown', 100],
    ['Tell something about you', 'about', 1000],
  ] as const)('edits %s with a code point limit', async (label, key, limit) => {
    const user = userEvent.setup();
    mockProfile(profileFor(member, { hometown: 'London', about: 'First line\nSecond line' }));
    let saved: Record<string, string> | undefined;
    server.use(http.put('/api/me/profile', async ({ request }) => {
      saved = (await request.json()) as Record<string, string>;
      return HttpResponse.json(saved);
    }));
    renderProfile('7');

    await user.click(await screen.findByRole('button', { name: `Edit ${label}` }));
    const input = screen.getByRole('textbox', { name: label });
    expect(input.tagName).toBe(key === 'about' ? 'TEXTAREA' : 'INPUT');
    await user.clear(input);
    const atLimit = '🌱'.repeat(limit);
    await user.click(input);
    await user.paste(atLimit + '🌱');
    expect(input).toHaveValue(atLimit);
    await user.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(input).not.toBeInTheDocument());
    expect(saved).toEqual({ nickname: '', hometown: 'London', about: 'First line\nSecond line', [key]: atLimit });
    if (key === 'nickname') {
      expect(screen.getByRole('heading', { name: `Ada "${atLimit}" Lovelace` })).toBeInTheDocument();
    }
  });

  it('shows a visitor filled fields and the status, with no owner controls', async () => {
    mockProfile({ ...colleague, nickname: 'Al', hometown: '', about: 'Two\nlines', status: 'In a meeting' });
    renderProfile('9');

    expect(await screen.findByRole('heading', { name: 'Alan "Al" Turing' })).toBeInTheDocument();
    expect(screen.getByText('Two lines')).toHaveStyle({ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' });
    expect(screen.getByText('In a meeting')).toBeInTheDocument();
    expect(screen.queryByText('Where are you from?')).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /status/i })).not.toBeInTheDocument();
    expect(screen.queryByRole('checkbox', { name: 'Allow anonymous messages on my wall' })).not.toBeInTheDocument();
  });

  it('sets status with Enter, cancels with Escape, and clears with Save', async () => {
    const user = userEvent.setup();
    mockProfile(profileFor(member));
    const saved: string[] = [];
    server.use(http.put('/api/me/status', async ({ request }) => {
      const { status } = (await request.json()) as { status: string };
      saved.push(status);
      return HttpResponse.json({ status });
    }));
    renderProfile('7');

    await user.click(await screen.findByRole('button', { name: 'Set status' }));
    const status = screen.getByRole('textbox', { name: 'Status' });
    expect(status).toHaveFocus();
    const atLimit = '🌱'.repeat(140);
    await user.paste(atLimit + '🌱');
    await user.keyboard('{Enter}');
    await user.click(await screen.findByRole('button', { name: `Edit status: ${atLimit}` }));
    await user.clear(screen.getByRole('textbox', { name: 'Status' }));
    await user.keyboard('{Escape}');
    expect(saved).toEqual([atLimit]);

    await user.click(screen.getByRole('button', { name: `Edit status: ${atLimit}` }));
    await user.clear(screen.getByRole('textbox', { name: 'Status' }));
    await user.click(screen.getByRole('button', { name: 'Save' }));
    expect(await screen.findByRole('button', { name: 'Set status' })).toBeInTheDocument();
    expect(saved).toEqual([atLimit, '']);
  });

  it('updates the wall setting without disabling the owner’s own composer', async () => {
    const user = userEvent.setup();
    mockProfile(profileFor(member));
    let saved: { allowAnonymous: boolean } | undefined;
    server.use(http.put('/api/me/wall-settings', async ({ request }) => {
      saved = (await request.json()) as { allowAnonymous: boolean };
      return HttpResponse.json(saved);
    }));
    renderProfile('7');

    const setting = await screen.findByRole('checkbox', { name: 'Allow anonymous messages on my wall' });
    expect(setting).toBeChecked();
    await user.click(setting);
    await waitFor(() => expect(setting).not.toBeChecked());
    expect(saved).toEqual({ allowAnonymous: false });
    expect(screen.getByRole('checkbox', { name: 'Post anonymously' })).toBeEnabled();
  });
});

function galleryComment(id: number, body: string, canDelete: boolean): PhotoComment {
  return {
    id, body, canDelete, createdAt: '2026-11-26T15:00:00Z',
    author: { id: canDelete ? member.id : colleague.id, name: canDelete ? member.name : colleague.name, avatarUrl: null },
  };
}

function pauseResponse() {
  let resolve!: () => void;
  const promise = new Promise<void>((done) => { resolve = done; });
  return { promise, resolve };
}

describe('photo interactions', () => {
  it('shows nonzero thumbnail counts, likes optimistically, and rolls back a rate-limited unlike', async () => {
    const user = userEvent.setup();
    let photo = galleryPhoto(3, { likeCount: 2, commentCount: 1 });
    mockProfile({ ...colleague, photos: [photo, galleryPhoto(4)] });
    const liked = pauseResponse();
    const unliked = pauseResponse();
    server.use(
      http.get('/api/users/:id', () => HttpResponse.json({ ...colleague, photos: [photo, galleryPhoto(4)] })),
      http.put('/api/photos/3/like', async () => {
        await liked.promise;
        photo = { ...photo, likeCount: 4, likedByMe: true };
        return HttpResponse.json({ likeCount: 4, likedByMe: true });
      }),
      http.delete('/api/photos/3/like', async () => {
        await unliked.promise;
        return HttpResponse.json({ code: 'rate_limited', message: 'Try again in a moment.' }, { status: 429 });
      }),
    );
    renderProfile('9');

    expect(await screen.findByLabelText('2 likes')).toHaveTextContent('♥ 2');
    expect(screen.getByLabelText('1 comment')).toHaveTextContent('💬 1');
    expect(screen.queryByLabelText('0 likes')).not.toBeInTheDocument();
    expect(screen.queryByLabelText('0 comments')).not.toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Open photo 1' }));
    const viewer = within(await screen.findByRole('dialog', { name: 'Lightbox' }));
    await user.click(await viewer.findByRole('button', { name: 'Like' }));
    expect(await viewer.findByRole('button', { name: 'Unlike' })).toBeDisabled();
    expect(viewer.getByLabelText('3 likes')).toHaveTextContent('3');
    liked.resolve();
    expect(await viewer.findByLabelText('4 likes')).toHaveTextContent('4');
    await waitFor(() => expect(viewer.getByRole('button', { name: 'Unlike' })).toBeEnabled());

    await user.click(viewer.getByRole('button', { name: 'Unlike' }));
    expect(await viewer.findByLabelText('3 likes')).toHaveTextContent('3');
    unliked.resolve();
    expect(await viewer.findByRole('alert')).toHaveTextContent('Try again in a moment.');
    expect(viewer.getByRole('button', { name: 'Unlike' })).toBeEnabled();
    expect(viewer.getByLabelText('4 likes')).toHaveTextContent('4');
  });

  it('lists comments, shows validation, appends and clears after posting, and removes deletable comments', async () => {
    const user = userEvent.setup();
    const readOnly = galleryComment(1, 'First\nline', false);
    const posted = galleryComment(2, 'New comment', true);
    let saved = [readOnly];
    const refetched = pauseResponse();
    mockProfile({ ...colleague, photos: [galleryPhoto(3, { commentCount: 1 })] });
    server.use(
      http.get('/api/photos/3/comments', async () => {
        if (saved.length === 2) await refetched.promise;
        return HttpResponse.json({ comments: saved });
      }),
      http.post('/api/photos/3/comments', async ({ request }) => {
        const { body } = (await request.json()) as { body: string };
        if (body !== posted.body) {
          return HttpResponse.json({ code: 'too_long', message: 'Comment must be 1000 characters or fewer.' }, { status: 400 });
        }
        saved = [...saved, posted];
        return HttpResponse.json(posted, { status: 201 });
      }),
      http.delete('/api/photos/3/comments/2', () => {
        saved = [readOnly];
        return new HttpResponse(null, { status: 204 });
      }),
    );
    renderProfile('9');
    await user.click(await screen.findByRole('button', { name: 'Open photo 1' }));
    const viewer = within(await screen.findByRole('dialog', { name: 'Lightbox' }));
    expect(await viewer.findByRole('link', { name: colleague.name })).toHaveAttribute('href', '/u/9');
    expect(viewer.getByText('First line')).toHaveTextContent('First line');
    expect(viewer.queryByRole('button', { name: /Delete comment/ })).not.toBeInTheDocument();

    const input = viewer.getByRole('textbox', { name: 'Write a comment' });
    await user.type(input, 'Oversized');
    await user.click(viewer.getByRole('button', { name: 'Comment' }));
    expect(await viewer.findByRole('alert')).toHaveTextContent('Comment must be 1000 characters or fewer.');
    expect(input).toHaveValue('Oversized');
    await user.clear(input);
    await user.type(input, posted.body);
    await user.click(viewer.getByRole('button', { name: 'Comment' }));

    expect(await viewer.findByText(posted.body)).toBeInTheDocument();
    expect(input).toHaveValue('');
    expect(viewer.getByRole('heading', { name: 'Comments (2)' })).toBeInTheDocument();
    refetched.resolve();
    await user.click(viewer.getByRole('button', { name: `Delete comment by ${member.name}` }));
    await waitFor(() => expect(viewer.queryByText(posted.body)).not.toBeInTheDocument());
    expect(viewer.getByRole('heading', { name: 'Comments (1)' })).toBeInTheDocument();
  });
});
