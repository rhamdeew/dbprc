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
