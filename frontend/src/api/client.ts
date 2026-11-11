import { notifications } from '@mantine/notifications';

/** JSON error body returned by the API. */
export type ApiErrorBody = {
  code: string;
  message: string;
};

export class ApiError extends Error implements ApiErrorBody {
  readonly status: number;
  readonly code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
  }
}

export type ApiRequest = Omit<RequestInit, 'body' | 'credentials'> & {
  body?: unknown;
};

const FALLBACK_MESSAGE = 'Something went wrong';

function notify(message: string) {
  notifications.show({ color: 'red', message });
}

/** Full navigation so a 401 leaves any in-memory route state behind. */
export function redirectToLogin(): void {
  const path = `${window.location.pathname}${window.location.search}`;
  if (window.location.pathname === '/login') return;
  window.location.assign(`/login?next=${encodeURIComponent(path)}`);
}

function isRawBody(body: unknown): body is BodyInit {
  return body instanceof FormData || body instanceof URLSearchParams || body instanceof Blob;
}

function encodeBody(body: unknown): { payload?: BodyInit; json: boolean } {
  if (body === undefined) return { json: false };
  if (typeof body === 'string') return { payload: body, json: true };
  if (isRawBody(body)) return { payload: body, json: false };
  return { payload: JSON.stringify(body), json: true };
}

async function readError(res: Response, fallbackCode: string): Promise<ApiErrorBody> {
  try {
    const data: unknown = await res.json();
    if (!data || typeof data !== 'object') {
      return { code: fallbackCode, message: FALLBACK_MESSAGE };
    }
    const record = data as { code?: unknown; message?: unknown };
    const code = typeof record.code === 'string' && record.code !== '' ? record.code : fallbackCode;
    const message = typeof record.message === 'string' && record.message !== '' ? record.message : FALLBACK_MESSAGE;
    return { code, message };
  } catch {
    return { code: fallbackCode, message: FALLBACK_MESSAGE };
  }
}

/**
 * `fetch` with cookies, JSON, and the API error shape.
 * 401 sends the browser to `/login?next=<path>`. Any other failure shows a notification.
 */
export async function api<T>(path: string, init: ApiRequest = {}): Promise<T> {
  const { body, headers: initHeaders, ...rest } = init;
  const headers = new Headers(initHeaders);
  if (!headers.has('Accept')) headers.set('Accept', 'application/json');
  const { payload, json } = encodeBody(body);
  if (payload !== undefined && json && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json');
  }

  const res = await fetch(path, {
    ...rest,
    credentials: 'include',
    headers,
    body: payload,
  });

  return complete<T>(res);
}

/** POST a multipart `file` field. Uses XHR so the caller can show upload progress. */
export function uploadFile<T>(path: string, file: File, onProgress?: (percent: number) => void): Promise<T> {
  const body = new FormData();
  body.append('file', file);
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open('POST', path);
    xhr.withCredentials = true;
    xhr.setRequestHeader('Accept', 'application/json');
    xhr.upload.addEventListener('progress', (event) => {
      if (!onProgress || !event.lengthComputable) return;
      const percent = uploadPercent(event.loaded, event.total);
      if (percent !== null) onProgress(percent);
    });
    xhr.addEventListener('load', () => {
      if (xhr.status === 0) {
        reject(networkError());
        return;
      }
      complete<T>(responseFromXhr(xhr)).then(resolve, reject);
    });
    xhr.addEventListener('error', () => {
      reject(networkError());
    });
    xhr.send(body);
  });
}

/** 0–100, or null when the total size is unknown. */
export function uploadPercent(loaded: number, total: number): number | null {
  if (!Number.isFinite(loaded) || !Number.isFinite(total) || total <= 0) return null;
  return Math.min(100, Math.max(0, Math.round((loaded / total) * 100)));
}

async function complete<T>(res: Response): Promise<T> {
  if (res.status === 401) {
    const { code, message } = await readError(res, 'unauthenticated');
    redirectToLogin();
    throw new ApiError(res.status, code, message);
  }

  if (!res.ok) {
    const { code, message } = await readError(res, 'request_failed');
    notify(message);
    throw new ApiError(res.status, code, message);
  }

  if (res.status === 204) return undefined as T;
  const text = await res.text();
  if (text === '') return undefined as T;
  try {
    return JSON.parse(text) as T;
  } catch {
    notify(FALLBACK_MESSAGE);
    throw new ApiError(res.status, 'invalid_response', FALLBACK_MESSAGE);
  }
}

function networkError(): ApiError {
  notify(FALLBACK_MESSAGE);
  return new ApiError(0, 'request_failed', FALLBACK_MESSAGE);
}

function responseFromXhr(xhr: XMLHttpRequest): Response {
  const body = typeof xhr.responseText === 'string' ? xhr.responseText : '';
  return new Response(body, {
    status: xhr.status,
    statusText: xhr.statusText,
    headers: parseHeaders(xhr.getAllResponseHeaders()),
  });
}

function parseHeaders(raw: string): Headers {
  const headers = new Headers();
  for (const line of raw.trim().split(/[\r\n]+/)) {
    if (line === '') continue;
    const index = line.indexOf(':');
    if (index <= 0) continue;
    const name = line.slice(0, index).trim();
    const value = line.slice(index + 1).trim();
    if (name !== '') headers.append(name, value);
  }
  return headers;
}
