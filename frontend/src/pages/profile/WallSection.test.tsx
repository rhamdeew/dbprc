import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { MemoryRouter } from 'react-router';
import { setupVitestCanvasMock } from 'vitest-canvas-mock';
import { describe, expect, it } from 'vitest';
import { wallPageSize, type WallPost, type WallRoot } from '../../api/wall';
import { render } from '../../test/render';
import { server } from '../../test/server';
import { WallSection } from './WallSection';

setupVitestCanvasMock();

const hint = "The owner of this wall will still see it's you";

function root(overrides: Partial<WallRoot> = {}): WallRoot {
  return {
    id: 1,
    body: 'hello',
    drawingUrl: null,
    createdAt: new Date(Date.now() - 5 * 60_000 - 5_000).toISOString(),
    isAnonymous: false,
    author: { id: 3, name: 'Grace Hopper', avatarUrl: '/uploads/avatars/grace.png' },
    canDelete: false,
    replies: [],
    ...overrides,
  };
}

function createdPost(overrides: Partial<WallPost> = {}): WallPost {
  return {
    id: 50,
    body: 'hello wall',
    drawingUrl: null,
    createdAt: new Date().toISOString(),
    isAnonymous: false,
    author: { id: 7, name: 'Ada Lovelace', avatarUrl: null },
    canDelete: true,
    ...overrides,
  };
}

function renderWall(userId = 9) {
  // Author names link to profiles, so the wall needs a router.
  return render(
    <MemoryRouter>
      <WallSection userId={userId} />
    </MemoryRouter>,
  );
}

function mockWall(posts: WallRoot[]) {
  server.use(http.get('/api/users/:id/wall', () => HttpResponse.json({ posts })));
}

function drawStroke() {
  const canvas = screen.getByRole('application', { name: 'Drawing pad' });
  fireEvent.pointerDown(canvas, { clientX: 10, clientY: 10, pointerId: 1, pointerType: 'mouse', button: 0, buttons: 1, pressure: 0.5 });
  fireEvent.pointerMove(canvas, { clientX: 40, clientY: 30, pointerId: 1, pointerType: 'mouse', button: 0, buttons: 1, pressure: 0.5 });
  fireEvent.pointerUp(canvas, { clientX: 40, clientY: 30, pointerId: 1, pointerType: 'mouse', button: 0, buttons: 0, pressure: 0 });
}

// jsdom's Blob is emptied when undici serializes FormData. Rebuild the body so MSW sees the PNG.
function installMultipartFetch(): () => void {
  const real = window.fetch.bind(window);
  const wrapped = async (input: RequestInfo | URL, init?: RequestInit) => {
    if (!(init?.body instanceof FormData)) return real(input, init);
    const encoded = await encodeForm(init.body);
    const headers = new Headers(init.headers);
    headers.set('Content-Type', encoded.contentType);
    return real(input, { ...init, body: new Blob([encoded.body]), headers });
  };
  window.fetch = wrapped as typeof window.fetch;
  return () => {
    window.fetch = real;
  };
}

async function encodeForm(form: FormData): Promise<{ body: ArrayBuffer; contentType: string }> {
  const boundary = `----arc${Math.random().toString(16).slice(2)}`;
  const chunks: Uint8Array[] = [];
  const encoder = new TextEncoder();
  const push = (value: string) => chunks.push(encoder.encode(value));
  for (const [key, value] of form.entries()) {
    push(`--${boundary}\r\n`);
    if (typeof value === 'string') {
      push(`Content-Disposition: form-data; name="${key}"\r\n\r\n${value}\r\n`);
      continue;
    }
    const filename = 'name' in value && value.name !== '' ? value.name : 'blob';
    push(`Content-Disposition: form-data; name="${key}"; filename="${filename}"\r\n`);
    push(`Content-Type: ${value.type || 'application/octet-stream'}\r\n\r\n`);
    chunks.push(new Uint8Array(await value.arrayBuffer()));
    push('\r\n');
  }
  push(`--${boundary}--\r\n`);
  const buffer = new ArrayBuffer(chunks.reduce((sum, chunk) => sum + chunk.length, 0));
  const body = new Uint8Array(buffer);
  let offset = 0;
  for (const chunk of chunks) {
    body.set(chunk, offset);
    offset += chunk.length;
  }
  return { body: buffer, contentType: `multipart/form-data; boundary=${boundary}` };
}

function part(source: string, name: string): string | undefined {
  const start = source.indexOf(`name="${name}"`);
  if (start < 0) return undefined;
  const headerEnd = source.indexOf('\r\n\r\n', start);
  if (headerEnd < 0) return undefined;
  const valueEnd = source.indexOf('\r\n--', headerEnd + 4);
  return source.slice(headerEnd + 4, valueEnd);
}

describe('wall section', () => {
  it('posts text on the wall', async () => {
    const user = userEvent.setup();
    mockWall([]);
    let body: FormData | undefined;
    server.use(
      http.post('/api/users/:id/wall', async ({ request }) => {
        body = await request.formData();
        return HttpResponse.json(createdPost({ body: 'hello wall' }), { status: 201 });
      }),
    );
    renderWall();

    const composer = await screen.findByRole('form', { name: 'Wall composer' });
    expect(screen.getByText('No posts yet.')).toBeInTheDocument();
    expect(screen.getByText(hint)).toBeInTheDocument();
    expect(within(composer).getByRole('button', { name: 'Post' })).toBeDisabled();

    await user.type(within(composer).getByRole('textbox', { name: 'Write on this wall' }), '  hello wall  ');
    await user.click(within(composer).getByRole('button', { name: 'Post' }));

    expect(await screen.findByText('hello wall')).toBeInTheDocument();
    expect(screen.queryByText('No posts yet.')).not.toBeInTheDocument();
    expect(screen.getByText('Ada Lovelace')).toBeInTheDocument();
    expect(screen.getByText('just now')).toBeInTheDocument();
    await waitFor(() => expect(body).toBeDefined());
    expect(body?.get('body')).toBe('hello wall');
    expect(body?.get('anonymous')).toBe('false');
    expect(body?.get('drawing')).toBeNull();
    expect(body?.get('parentId')).toBeNull();
    expect(within(composer).getByRole('textbox', { name: 'Write on this wall' })).toHaveValue('');
  });

  it('posts a drawing from the pad', async () => {
    const user = userEvent.setup();
    const restore = installMultipartFetch();
    mockWall([]);
    let raw = '';
    server.use(
      http.post('/api/users/:id/wall', async ({ request }) => {
        raw = new TextDecoder().decode(await request.arrayBuffer());
        return HttpResponse.json(
          createdPost({ id: 51, body: '', drawingUrl: '/uploads/drawings/51.png' }),
          { status: 201 },
        );
      }),
    );
    renderWall();

    try {
      const composer = await screen.findByRole('form', { name: 'Wall composer' });
      expect(screen.queryByRole('application', { name: 'Drawing pad' })).not.toBeInTheDocument();
      await user.click(within(composer).getByRole('checkbox', { name: 'Draw' }));
      expect(screen.getByRole('application', { name: 'Drawing pad' })).toBeInTheDocument();
      drawStroke();
      await waitFor(() => expect(within(composer).getByRole('button', { name: 'Post' })).toBeEnabled());
      await user.click(within(composer).getByRole('button', { name: 'Post' }));

      expect(await screen.findByRole('img', { name: 'Drawing' })).toHaveAttribute('src', '/uploads/drawings/51.png');
      expect(screen.queryByRole('application', { name: 'Drawing pad' })).not.toBeInTheDocument();
      await waitFor(() => expect(raw).not.toBe(''));
      expect(part(raw, 'body')).toBe('');
      expect(part(raw, 'anonymous')).toBe('false');
      expect(part(raw, 'parentId')).toBeUndefined();
      expect(raw).toContain('name="drawing"; filename="drawing.png"');
      expect(raw).toContain('Content-Type: image/png');
      expect((part(raw, 'drawing') ?? '').length).toBeGreaterThan(0);
    } finally {
      restore();
    }
  });

  it('posts anonymously and warns that the owner can still see you', async () => {
    const user = userEvent.setup();
    mockWall([]);
    let anonymous: FormDataEntryValue | null = null;
    server.use(
      http.post('/api/users/:id/wall', async ({ request }) => {
        anonymous = (await request.formData()).get('anonymous');
        return HttpResponse.json(
          createdPost({ id: 52, body: 'quiet', isAnonymous: true, author: null, canDelete: false }),
          { status: 201 },
        );
      }),
    );
    renderWall();

    const composer = await screen.findByRole('form', { name: 'Wall composer' });
    expect(screen.getByText(hint)).toBeInTheDocument();
    await user.type(within(composer).getByRole('textbox', { name: 'Write on this wall' }), 'quiet');
    await user.click(within(composer).getByRole('checkbox', { name: 'Post anonymously' }));
    await user.click(within(composer).getByRole('button', { name: 'Post' }));

    expect(await screen.findByText('quiet')).toBeInTheDocument();
    expect(screen.getByText('Anonymous')).toBeInTheDocument();
    expect(screen.queryByText('Ada Lovelace')).not.toBeInTheDocument();
    await waitFor(() => expect(anonymous).toBe('true'));
  });

  it('replies on the root post', async () => {
    const user = userEvent.setup();
    const reply = root({
      id: 8,
      body: 'first reply',
      author: { id: 4, name: 'Alan Turing', avatarUrl: null },
      canDelete: false,
    });
    mockWall([root({ id: 4, body: 'root post', replies: [reply] })]);
    let parentId: FormDataEntryValue | null = null;
    server.use(
      http.post('/api/users/:id/wall', async ({ request }) => {
        const form = await request.formData();
        parentId = form.get('parentId');
        return HttpResponse.json(createdPost({ id: 60, body: 'thanks', canDelete: false }), { status: 201 });
      }),
    );
    renderWall();

    expect(await screen.findByText('root post')).toBeInTheDocument();
    expect(screen.getByText('first reply')).toBeInTheDocument();
    expect(screen.getAllByRole('button', { name: 'Reply' })).toHaveLength(1);

    await user.click(screen.getByRole('button', { name: 'Reply' }));
    const composer = screen.getByRole('form', { name: 'Reply composer' });
    expect(composer).toBeInTheDocument();
    expect(within(composer).getByText(hint)).toBeInTheDocument();
    await user.type(within(composer).getByRole('textbox', { name: 'Write a reply' }), 'thanks');
    await user.click(within(composer).getByRole('button', { name: 'Post' }));

    expect(await screen.findByText('thanks')).toBeInTheDocument();
    expect(screen.queryByRole('form', { name: 'Reply composer' })).not.toBeInTheDocument();
    expect(screen.getAllByRole('button', { name: 'Reply' })).toHaveLength(1);
    await waitFor(() => expect(parentId).toBe('4'));
  });

  it('hides Delete when canDelete is false', async () => {
    mockWall([
      root({
        id: 4,
        body: 'root post',
        canDelete: false,
        replies: [root({ id: 8, body: 'hidden reply', canDelete: false })],
      }),
    ]);
    renderWall();
    expect(await screen.findByText('root post')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Delete' })).not.toBeInTheDocument();
  });

  it('deletes a reply and a root after confirm', async () => {
    const user = userEvent.setup();
    mockWall([
      root({
        id: 4,
        body: 'root post',
        canDelete: true,
        replies: [root({ id: 8, body: 'a reply', canDelete: true })],
      }),
    ]);
    const deleted: string[] = [];
    server.use(
      http.delete('/api/wall/:postId', ({ params }) => {
        deleted.push(String(params.postId));
        return new HttpResponse(null, { status: 204 });
      }),
    );
    renderWall();

    expect(await screen.findByText('a reply')).toBeInTheDocument();
    const buttons = screen.getAllByRole('button', { name: 'Delete' });
    expect(buttons).toHaveLength(2);

    await user.click(buttons[1]!);
    const dialog = await screen.findByRole('dialog', { name: 'Delete this post?' });
    await user.click(within(dialog).getByRole('button', { name: 'Cancel' }));
    expect(deleted).toEqual([]);
    expect(screen.getByText('a reply')).toBeInTheDocument();

    await user.click(screen.getAllByRole('button', { name: 'Delete' })[1]!);
    await user.click(within(await screen.findByRole('dialog', { name: 'Delete this post?' })).getByRole('button', { name: 'Delete' }));
    await waitFor(() => expect(deleted).toEqual(['8']));
    expect(screen.queryByText('a reply')).not.toBeInTheDocument();
    expect(screen.getByText('root post')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Delete' }));
    await user.click(within(await screen.findByRole('dialog', { name: 'Delete this post?' })).getByRole('button', { name: 'Delete' }));
    await waitFor(() => expect(deleted).toEqual(['8', '4']));
    expect(screen.queryByText('root post')).not.toBeInTheDocument();
    expect(screen.getByText('No posts yet.')).toBeInTheDocument();
  });

  it('shows the author of an anonymous post to the wall owner', async () => {
    mockWall([
      root({
        id: 2,
        body: 'secret',
        isAnonymous: true,
        canDelete: true,
        drawingUrl: '/uploads/drawings/2.png',
        author: { id: 4, name: 'Ada Lovelace', avatarUrl: '/uploads/avatars/ada.png' },
      }),
    ]);
    renderWall(4);

    expect(await screen.findByText('Anonymous · Ada Lovelace')).toBeInTheDocument();
    // The owner sees the name, but an anonymous post does not link to the profile.
    expect(screen.queryByRole('link')).not.toBeInTheDocument();
    expect(screen.getByText('secret')).toBeInTheDocument();
    expect(screen.getByText('5 minutes ago')).toBeInTheDocument();
    const drawing = screen.getByRole('img', { name: 'Drawing' });
    expect(drawing).toHaveAttribute('src', '/uploads/drawings/2.png');
    // Shown at the size it was drawn, whatever the stored bitmap or a failed load would give.
    expect(drawing).toHaveStyle({ width: '400px', maxWidth: '100%' });
    expect(document.querySelector('img[src="/uploads/avatars/ada.png"]')).not.toBeNull();
    expect(screen.getByRole('button', { name: 'Delete' })).toBeInTheDocument();
  });

  it('hides an anonymous author from everyone else', async () => {
    mockWall([
      root({
        id: 2,
        body: 'secret',
        isAnonymous: true,
        author: null,
        canDelete: false,
      }),
      root({ id: 3, body: 'public note' }),
    ]);
    renderWall();

    expect(await screen.findByText('secret')).toBeInTheDocument();
    expect(screen.getByText('Anonymous')).toBeInTheDocument();
    expect(screen.queryByText(/Anonymous ·/)).not.toBeInTheDocument();
    expect(screen.queryByText('Ada Lovelace')).not.toBeInTheDocument();
    expect(screen.getByText('Grace Hopper')).toBeInTheDocument();
    // Only the signed post links to its author.
    expect(screen.getAllByRole('link')).toHaveLength(1);
    expect(screen.getByRole('link', { name: 'Grace Hopper' })).toHaveAttribute('href', '/u/3');
    expect(screen.getByText('public note')).toBeInTheDocument();
    expect(document.querySelector('img[src="/uploads/avatars/grace.png"]')).not.toBeNull();
    expect(screen.getAllByText('5 minutes ago')).toHaveLength(2);
    expect(screen.queryByRole('button', { name: 'Delete' })).not.toBeInTheDocument();
  });

  it('loads older posts with Show more', async () => {
    const user = userEvent.setup();
    const first = Array.from({ length: wallPageSize }, (_, index) =>
      root({ id: wallPageSize - index, body: `post ${wallPageSize - index}` }),
    );
    const oldestId = first[first.length - 1]?.id;
    let before: string | null = null;
    server.use(
      http.get('/api/users/:id/wall', ({ request }) => {
        const url = new URL(request.url);
        before = url.searchParams.get('before');
        expect(url.searchParams.get('limit')).toBe(String(wallPageSize));
        if (before === null) return HttpResponse.json({ posts: first });
        return HttpResponse.json({ posts: [root({ id: 1000, body: 'older post' })] });
      }),
    );
    renderWall();

    expect(await screen.findByText('post 20')).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Show more' }));
    expect(await screen.findByText('older post')).toBeInTheDocument();
    expect(before).toBe(String(oldestId));
    expect(screen.queryByRole('button', { name: 'Show more' })).not.toBeInTheDocument();
  });

  it('shows a loader, then a wall error', async () => {
    let release: (response: Response) => void = () => {};
    const pending = new Promise<Response>((resolve) => {
      release = resolve;
    });
    server.use(http.get('/api/users/:id/wall', () => pending));
    renderWall();

    expect(screen.getByLabelText('Loading wall')).toBeInTheDocument();
    release(HttpResponse.json({ code: 'internal', message: 'Something went wrong.' }, { status: 500 }));

    expect(await screen.findByText('Something went wrong.')).toBeInTheDocument();
    expect(screen.queryByRole('form', { name: 'Wall composer' })).not.toBeInTheDocument();
  });
});
