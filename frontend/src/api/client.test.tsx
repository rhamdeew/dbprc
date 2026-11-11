import { act, screen } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render } from '../test/render';
import { locationAssign, setTestPath } from '../test/location';
import { server } from '../test/server';
import { api, ApiError, uploadFile, uploadPercent } from './client';

async function capture(run: () => Promise<unknown>) {
  let caught: unknown;
  await act(async () => {
    try {
      await run();
    } catch (err) {
      caught = err;
    }
  });
  return caught;
}

describe('api client', () => {
  beforeEach(() => {
    render(<div />);
  });

  it('sends cookies and JSON', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch');
    server.use(http.post('/api/echo', () => HttpResponse.json({ ok: true })));

    await expect(api('/api/echo', { method: 'POST', body: { ok: true } })).resolves.toEqual({ ok: true });

    const init = fetchSpy.mock.calls.at(-1)?.[1] as RequestInit;
    expect(init.credentials).toBe('include');
    expect(init.method).toBe('POST');
    expect(init.body).toBe(JSON.stringify({ ok: true }));
    const headers = new Headers(init.headers);
    expect(headers.get('Accept')).toBe('application/json');
    expect(headers.get('Content-Type')).toBe('application/json');
    fetchSpy.mockRestore();
  });

  it('does not force JSON content type for a raw body', async () => {
    let contentType: string | null = 'unset';
    server.use(
      http.post('/api/upload', ({ request }) => {
        contentType = request.headers.get('content-type');
        return HttpResponse.json({ ok: true });
      }),
    );
    const form = new FormData();
    form.append('file', 'hi');
    await api('/api/upload', { method: 'POST', body: form });
    expect(contentType?.includes('application/json') ?? false).toBe(false);
  });

  it('returns undefined for an empty success body', async () => {
    server.use(http.delete('/api/item', () => new HttpResponse(null, { status: 204 })));
    await expect(api('/api/item', { method: 'DELETE' })).resolves.toBeUndefined();

    server.use(http.get('/api/empty', () => new HttpResponse('', { status: 200 })));
    await expect(api('/api/empty')).resolves.toBeUndefined();
  });

  it('maps a JSON error to a typed error and a notification', async () => {
    server.use(
      http.get('/api/item', () => HttpResponse.json({ code: 'not_found', message: 'That page is gone' }, { status: 404 })),
    );
    const error = await capture(() => api('/api/item'));
    expect(error).toBeInstanceOf(ApiError);
    expect(error).toMatchObject({ status: 404, code: 'not_found', message: 'That page is gone' });
    expect(await screen.findByRole('alert')).toHaveTextContent('That page is gone');
  });

  it('fills in a missing code or message', async () => {
    server.use(http.get('/api/item', () => HttpResponse.json({ message: 'Nope' }, { status: 400 })));
    await expect(capture(() => api('/api/item'))).resolves.toMatchObject({ code: 'request_failed', message: 'Nope' });

    server.use(http.get('/api/item', () => HttpResponse.json({ code: '' }, { status: 400 })));
    await expect(capture(() => api('/api/item'))).resolves.toMatchObject({
      code: 'request_failed',
      message: 'Something went wrong',
    });

    server.use(http.get('/api/item', () => HttpResponse.json(['nope'], { status: 500 })));
    await expect(capture(() => api('/api/item'))).resolves.toMatchObject({
      code: 'request_failed',
      message: 'Something went wrong',
    });

    server.use(http.get('/api/item', () => HttpResponse.json(null, { status: 400 })));
    await expect(capture(() => api('/api/item'))).resolves.toMatchObject({
      code: 'request_failed',
      message: 'Something went wrong',
    });
  });

  it('maps a non-JSON error body', async () => {
    server.use(http.get('/api/item', () => new HttpResponse('<html>nope</html>', { status: 502 })));
    await expect(capture(() => api('/api/item'))).resolves.toMatchObject({
      status: 502,
      code: 'request_failed',
      message: 'Something went wrong',
    });
    expect(await screen.findByRole('alert')).toHaveTextContent('Something went wrong');
  });

  it('maps an invalid success payload', async () => {
    server.use(http.get('/api/item', () => new HttpResponse('not-json', { status: 200 })));
    await expect(capture(() => api('/api/item'))).resolves.toMatchObject({
      code: 'invalid_response',
      message: 'Something went wrong',
    });
    expect(await screen.findByRole('alert')).toHaveTextContent('Something went wrong');
  });

  it('redirects to login on 401 and does not notify', async () => {
    setTestPath('/messenger', '?x=1');
    server.use(http.get('/api/me', () => new HttpResponse(null, { status: 401 })));

    await expect(api('/api/me')).rejects.toMatchObject({ status: 401, code: 'unauthenticated', message: 'Something went wrong' });
    expect(locationAssign).toHaveBeenCalledWith('/login?next=%2Fmessenger%3Fx%3D1');
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('keeps the error code from a 401 body', async () => {
    setTestPath('/u/3');
    server.use(
      http.get('/api/me', () => HttpResponse.json({ code: 'unauthenticated', message: 'Sign in required' }, { status: 401 })),
    );
    await expect(api('/api/me')).rejects.toMatchObject({ code: 'unauthenticated', message: 'Sign in required' });
    expect(locationAssign).toHaveBeenCalledWith('/login?next=%2Fu%2F3');
  });

  it('does not redirect again when the page is already login', async () => {
    setTestPath('/login', '?next=%2F');
    server.use(http.get('/api/me', () => new HttpResponse(null, { status: 401 })));
    await expect(api('/api/me')).rejects.toBeInstanceOf(ApiError);
    expect(locationAssign).not.toHaveBeenCalled();
  });

  it('sends a string body as JSON', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch');
    server.use(http.post('/api/echo', () => HttpResponse.json({ ok: true })));
    await api('/api/echo', { method: 'POST', body: '{"a":1}' });
    const init = fetchSpy.mock.calls.at(-1)?.[1] as RequestInit;
    expect(new Headers(init.headers).get('Content-Type')).toBe('application/json');
    expect(init.body).toBe('{"a":1}');
    fetchSpy.mockRestore();
  });
});

describe('uploadFile', () => {
  beforeEach(() => {
    render(<div />);
  });

  it('posts the file field and returns the JSON body', async () => {
    const appended: unknown[][] = [];
    const append = FormData.prototype.append;
    const spy = vi.spyOn(FormData.prototype, 'append').mockImplementation(function (this: FormData, ...args) {
      appended.push(args);
      return append.apply(this, args);
    });
    server.use(http.post('/api/me/avatar', () => HttpResponse.json({ avatarUrl: '/uploads/avatars/a.png' })));

    const file = new File(['png'], 'a.png', { type: 'image/png' });
    await expect(uploadFile('/api/me/avatar', file, () => {})).resolves.toEqual({ avatarUrl: '/uploads/avatars/a.png' });
    expect(appended).toEqual([['file', file]]);
    spy.mockRestore();
  });

  it('shows an upload error', async () => {
    server.use(
      http.post('/api/me/avatar', () =>
        HttpResponse.json(
          { code: 'unsupported_type', message: 'Use a JPEG, PNG, GIF, or WebP image.' },
          { status: 400 },
        ),
      ),
    );
    const error = await capture(() => uploadFile('/api/me/avatar', new File(['x'], 'a.gif', { type: 'image/gif' })));
    expect(error).toMatchObject({ status: 400, code: 'unsupported_type' });
    expect(await screen.findByRole('alert')).toHaveTextContent('Use a JPEG, PNG, GIF, or WebP image.');
  });

  it('redirects to login when the upload is unauthorized', async () => {
    setTestPath('/u/7');
    server.use(http.post('/api/me/avatar', () => new HttpResponse(null, { status: 401 })));
    await expect(uploadFile('/api/me/avatar', new File(['x'], 'a.png', { type: 'image/png' }))).rejects.toMatchObject({
      status: 401,
      code: 'unauthenticated',
    });
    expect(locationAssign).toHaveBeenCalledWith('/login?next=%2Fu%2F7');
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('rejects an invalid success payload', async () => {
    server.use(http.post('/api/me/avatar', () => new HttpResponse('not-json', { status: 200 })));
    await expect(capture(() => uploadFile('/api/me/avatar', new File(['x'], 'a.png')))).resolves.toMatchObject({
      code: 'invalid_response',
    });
    expect(await screen.findByRole('alert')).toHaveTextContent('Something went wrong');
  });
});

describe('uploadPercent', () => {
  it('clamps a known size and ignores an unknown total', () => {
    expect(uploadPercent(1, 4)).toBe(25);
    expect(uploadPercent(4, 4)).toBe(100);
    expect(uploadPercent(8, 4)).toBe(100);
    expect(uploadPercent(0, 0)).toBeNull();
    expect(uploadPercent(1, Number.NaN)).toBeNull();
  });
});
