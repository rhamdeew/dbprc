/** Friendly copy for the codes `GET /auth/google/callback` puts on `/login?error=`. */
const LOGIN_ERRORS: Record<string, string> = {
  domain: 'Use your company Google account',
  unverified: 'Your Google email is not verified.',
  state: 'Sign-in expired. Try again.',
  provider: 'Google sign-in is unavailable. Try again.',
};

const UNKNOWN_LOGIN_ERROR = 'Sign-in failed. Try again.';

export function loginErrorMessage(code: string | null): string | null {
  if (!code) return null;
  return LOGIN_ERRORS[code] ?? UNKNOWN_LOGIN_ERROR;
}

/** Google start URL. `next` is the login page's query value, passed through as-is. */
export function googleStartPath(next: string | null): string {
  if (!next) return '/auth/google/start';
  return `/auth/google/start?${new URLSearchParams({ next }).toString()}`;
}
