# Security

ARC is an internal app for about 300 colleagues. Each environment has one API process and one SQLite file. The people to worry about are an anonymous caller on the internet, a signed-in colleague, and an operator who can read the database.

## Gate

Internet traffic terminates TLS on the edge Caddy. The environment Caddy (`deploy/env/Caddyfile`) then requires Basic Auth on every path except `/healthz`. That password is shared for the whole environment. It keeps the site off the public internet. It does not say who the caller is.

Identity is Google only. `GET /auth/google/callback` accepts an ID token when the audience and issuer match, the nonce matches, `email_verified` is true, and the `hd` claim equals `ALLOWED_DOMAIN`. The `hd` on the authorize URL is not the check. There are no passwords. The first accepted login creates the account. Name and email change only on a later login.

The session cookie (`arc_sid`) is HttpOnly and SameSite=Lax, and Secure outside dev. It is an opaque server-side token. A mutating request must send `Origin` equal to `APP_BASE_URL`, or, when `Origin` is absent, `Sec-Fetch-Site: same-origin`.

## Anonymity

A wall post can be marked anonymous. The author is always stored. The JSON `author` object is left out for everyone except the wall owner, and `isAnonymous` stays true, so the owner can see that the post was anonymous and who wrote it. The composer says so before the post is sent. Replies follow the same rule. Chat messages, profiles, and search are never anonymous: the other person sees the real name. Anyone who can read `wall_posts.author_id`, including an operator with the database, can see the author. The API does not log message bodies or emails.

## Admin

`ADMIN_EMAILS` is the only admin list. `GET /api/me` returns `isAdmin` for those addresses. Admins sign in with the same Google flow as everyone else. The product rule is aggregate stats, plus hard-delete of one profile by exact email (their wall posts, posts they wrote, and whole chats), with no directory browsing and no impersonation. A non-admin call to an admin API is 404. That delete API is not built yet: `/admin` is a placeholder, and `isAdmin` is the only admin check in this build. A user removed from `users` can sign in again and gets an empty profile.

## Abuse limits

JSON routes accept at most 32KiB. Routes that do not read a body accept at most 1KiB. Avatar, photo, and wall uploads accept at most 10MB plus 1MB of multipart framing. A larger body is 400 `{"code":"too_large"}`. Per signed-in user, per minute: random chat 10, wall posts 20 (every POST, including a rejected one), chat messages 60, and avatar plus photo uploads 20 together. The next call is 429 `{"code":"rate_limited"}` with `Retry-After`. The counters live in the API process.

The web server sends `Content-Security-Policy: default-src 'self'; img-src 'self' data: blob:; connect-src 'self' wss://{host}; frame-ancestors 'none'`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: same-origin`, and HSTS `max-age=31536000`. `/uploads` is `Content-Disposition: inline` and `nosniff`. Dev and end-to-end tests send the same policy except HSTS, which is Caddy-only. CI runs `govulncheck ./...` (Go 1.27.4, `golang.org/x/image` v0.45.0) and `npm audit --audit-level=high`.
