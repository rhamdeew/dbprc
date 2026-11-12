# ARC-38 full E2E audit

Audited 2026-11-12 against `14b0ad716cf9df4a4871265a48ce0c94df281b26` (main after ARC-37). **7 confirmed findings: S1 0, S2 5, S3 2. No product code changed.**

## Summary

| ID | Severity | Area | Finding | Backlog card |
|---|---|---|---|---|
| F01 | S2 | Theme / responsive layout | Allow Mantine styles under the active CSP | [#30 · ARC-39](#card-30) |
| F02 | S2 | Unread messages | Keep background chat messages unread | [#31 · ARC-40](#card-31) |
| F03 | S2 | Upload validation | Return a validation error for truncated PNG uploads | [#32 · ARC-41](#card-32) |
| F04 | S2 | Admin routing | Show the specified not-found page to non-admins | [#33 · ARC-42](#card-33) |
| F05 | S3 | Mobile navigation | Keep top-bar search and navigation inside the mobile viewport | [#34 · ARC-43](#card-34) |
| F06 | S3 | Profile text layout | Wrap long unbroken interest text | [#35 · ARC-44](#card-35) |
| F07 | S2 | Profile Unicode validation | Count interest limits by Unicode characters in the editor | [#36 · ARC-45](#card-36) |

The table links to the corresponding card records below. The board API returns numbers/IDs but no canonical browser URLs, so none are invented. All seven cards exist in **Project Board 133 → Backlog**, not Queue. Each has its screenshot attached. Start review with **F01 (mobile layout/CSP), F02 (lost unread state), F03 (upload validation)**.

## Method and evidence

Expected requirements were written before browser execution from all 25 ARC cards in Review/Done and their human comments, followed by `docs/ARCHITECTURE.md` and `AGENTS.md`. `sources.json` preserves those descriptions/comments; only ARC-09 had a human override (both nightly environments, actual bucket names and no optional ping). HANDOFF was read first as the code map, not used to override card requirements.

Browsers used the exact `scripts/e2e.sh` topology: built frontend through `STATIC_DIR`, mockoidc on 9999, API on 18080, isolated temporary SQLite/uploads, dev-only message key, and admin@example.com as ADMIN_EMAILS. Independent Playwright drivers used two normal contexts, an admin, a foreign-domain identity and an unverified identity; additional temporary users tested authorization and search limits. Desktop/mobile viewports were 1280×800 and 390×844. Only local mock identities and disposable data were modified.

Evidence bundle: workflow run **61061**, collected files under `/workspace/outputs/ARC-38/`. `browser-results.json`, `supplement-results.json`, `coverage-results.json`, and `final-probes-results.json` contain API bodies, geometry and observations. PNG names below are paths relative to that directory and also attached to their finding cards. `audit.cjs`, `supplement.cjs`, `coverage.cjs`, `final-probes.cjs` and the make/script wrappers preserve how probes ran. These exploratory collectors are not committed tests. Harness-only timeouts (waiting for a profile while on messenger, clicking disabled Clear) and a route-handler teardown race were excluded; subsequent focused runs covered the affected behavior. A 10MB Playwright argument-serialization stall was rerun using a multipart API request.

Visibility and browser notifications were instrumented in headless Chromium: `document.hidden` and a constructor/permission spy, with real app socket events and real server read cursors. This proves application behavior, not native OS popup delivery. The CSP-removal diagnostic changed only one intercepted browser response to isolate causes; all findings and their principal screenshots use the normal restrictive CSP. Synthetic same-task double clicks caused duplicate submissions, but realistic Playwright double-clicks did not, so no duplicate-submit bug card was created.

Validation: `make lint test` **passed with exit 0** (Go 82.8%, frontend lines 96.3%); `make e2e` **9/9 passed**; `make generate` checked idempotence. No failing/skipped tests or product fixes were added. Production R2, secrets, TLS/Basic Auth and native notification behavior remain explicitly unverified.

## Findings

<a id="card-30"></a>
### F01 · S2 · Allow Mantine styles under the active CSP

Backlog **#30 / ARC-39**, task ID `8398`. Priority Medium. Depends: ARC-14, ARC-27, ARC-31.

**Reproduce:** Fresh company login; open /u/<own id> at 1280×800, then /messenger/<chat id> at 390×844. Reload and repeat.

**Expected / source:** ARC-14 requires Tahoma/Verdana 12px, an unobscured blue shell header and the 2006 theme; ARC-27 requires messenger panes to stack on mobile. ARC-31 requires E2E to remain usable with CSP.

**Actual:** CSP blocks generated inline style elements. Body uses system font at 16px; header is 31px and overlays the heading; mobile messenger remains flex-direction: row and compresses the thread/composer into a narrow strip. A diagnostic response with only CSP removed restores Tahoma 12px and mobile column layout; no product files were edited.

**Evidence / confirmation:** supplement-results.json: theme 1/2; browser-results.json: mobile messenger layout (flex=row). Console: Applying inline style violates ... default-src 'self' ... action has been blocked. Screenshots: plain-profile-desktop-1.png, messenger-mobile.png. Confirmed on two reloads and an independent run. Board attachment IDs: 40788, 40789.

**Fix-card acceptance:** Keep a restrictive CSP while allowing the required generated styles (for example a correctly wired nonce). Add a browser regression that fails before the fix: verify computed Tahoma 12px, unobscured heading, mobile flex column and no CSP style violations. Existing E2E must pass.

<a id="card-31"></a>
### F02 · S2 · Keep background chat messages unread

Backlog **#31 / ARC-40**, task ID `8399`. Priority Medium. Depends: ARC-27, ARC-28.

**Reproduce:** Fresh logins as two colleagues; start a random chat and open it in both contexts. Background A's tab while keeping the thread route open. Send a message from B. Query GET /api/chats as A before returning to the tab. Repeat with another message.

**Expected / source:** ARC-27 says mark read when the thread is visible; ARC-28 requires unread title/badges. A hidden thread must not acknowledge newly arrived messages until visible.

**Actual:** The hidden thread immediately posts the read cursor; server unreadCount is 0 and the title remains ARC. This is not only a local badge problem. The audit forced document.hidden=true in headless Chromium and observed the same behavior twice; a hidden notification was produced on the granted-permission run.

**Evidence / confirmation:** supplement-results.json: hidden read 1 (sent id 1, unreadCount 0), hidden read 2 (sent id 2, unreadCount 0); browser-results.json: hidden read count. Screenshot hidden-unread-1.png. Headless visibility instrumentation is explicit in supplement.cjs. Board attachment IDs: 40790.

**Fix-card acceptance:** Gate marking read on actual document/thread visibility and acknowledge on return. Add a regression with hidden document plus an open thread: incoming messages retain server unread count and title badge; restoring visibility advances the cursor.

<a id="card-32"></a>
### F03 · S2 · Return a validation error for truncated PNG uploads

Backlog **#32 / ARC-41**, task ID `8400`. Priority Medium. Depends: ARC-18, ARC-19.

**Reproduce:** Fresh company login, own profile, Add photo. Upload the first 24 bytes of e2e/fixtures/photo.png as truncated.png with MIME image/png. Repeat after reload.

**Expected / source:** ARC-18 requires corrupt images to be rejected safely; ARC-19 and AGENTS.md require the right HTTP status and friendly validation error (400 invalid_image).

**Actual:** POST /api/me/photos returns HTTP 500 with {"code":"internal","message":"Something went wrong."}. The UI shows a generic server-error toast instead of an invalid-image message. Three independent submissions reproduced it.

**Evidence / confirmation:** browser-results.json: invalid upload bad.png; supplement-results.json: corrupt PNG 1/2, each status 500 and the same body. Screenshots corrupt-upload-1.png and corrupt-upload-2.png. Board attachment IDs: 40791.

**Fix-card acceptance:** Map truncated/corrupt decoder errors to 400 invalid_image; leave no photo row or orphaned upload. Add a handler regression using this exact 24-byte fixture that fails before the fix, plus a friendly UI error assertion.

<a id="card-33"></a>
### F04 · S2 · Show the specified not-found page to non-admins

Backlog **#33 / ARC-42**, task ID `8401`. Priority Medium. Depends: ARC-35, ARC-36.

**Reproduce:** Fresh non-admin company login. Navigate directly to /admin. Repeat after a reload and also open an unknown route.

**Expected / source:** ARC-35 explicitly requires the normal 404 page for non-admin /admin; ARC-36 says a non-admin opening /admin sees 404. No human comment overrides those requirements.

**Actual:** The router silently navigates to /, then /u/<own id>, and renders the user's profile. Unknown routes behave the same way. API protection correctly returns 404; this finding concerns the UI. HANDOFF.md documents the implementation but has lower authority than these cards.

**Evidence / confirmation:** browser-results.json: nonadmin page and non-admin repeat 1; supplement-results.json: non-admin 404 repeat 1/2. Screenshot nonadmin-repeat-1.png. Board attachment IDs: 40792.

**Fix-card acceptance:** Render a neutral not-found page for non-admin /admin and unknown routes without showing admin content. Update the existing E2E expectation that currently codifies redirect-home. Add a regression that fails before the change.

<a id="card-34"></a>
### F05 · S3 · Keep top-bar search and navigation inside the mobile viewport

Backlog **#34 / ARC-43**, task ID `8402`. Priority Low. Depends: ARC-14, ARC-24.

**Reproduce:** Fresh company login, set viewport to 390×844, open own profile and /messenger. Try to access the right edge of colleague search. Reload and repeat.

**Expected / source:** ARC-14 app-shell navigation and ARC-24 search must stay usable; ARC-38 explicitly checks overflow and clipped controls at 390×844.

**Actual:** Search starts at x≈237 and ends at x≈417, beyond the 390px viewport; normal profile document width is 406px. Nav labels wrap inside a short fixed top bar. The clipping also persists in the diagnostic run with CSP removed (right edge 416px), so correcting Mantine CSP alone will not fix this.

**Evidence / confirmation:** supplement-results.json: mobile 1/2, diagnostic without CSP mobile. Screenshots plain-profile-mobile-1.png and plain-profile-mobile-2.png; diagnostic-without-csp-mobile.png is supporting isolation evidence. Board attachment IDs: 40793.

**Fix-card acceptance:** Add a responsive shell arrangement so all nav/search controls are visible and keyboard reachable at 390px, including admin navigation. A Playwright geometry check must fail before the fix and pass without horizontal overflow afterward.

<a id="card-35"></a>
### F06 · S3 · Wrap long unbroken interest text

Backlog **#35 / ARC-44**, task ID `8403`. Priority Low. Depends: ARC-20.

**Reproduce:** Fresh company login; edit Hobby to 500 ASCII A characters, Save, reload. View at 1280×800 and 390×844. Repeat the save/reload.

**Expected / source:** ARC-19 permits 500-character interests; ARC-20 must render them in the information panel; ARC-38 checks very long text and overflow at both sizes.

**Actual:** The accepted interest remains one unbroken line and expands document scroll width to 5855px on desktop and 5705px on mobile. Text spills outside the information panel and obscures the intended layout. Reproduced twice.

**Evidence / confirmation:** supplement-results.json: long text desktop 1/2 and long text mobile 1/2. Screenshots long-ascii-desktop-1.png and long-ascii-mobile-1.png. Board attachment IDs: 40794, 40795.

**Fix-card acceptance:** Wrap unbroken user text within its panel without truncating the stored value. Add a browser regression using a 500-character word at both specified viewports; document scroll width must stay within the viewport after known shell issues are addressed.

<a id="card-36"></a>
### F07 · S2 · Count interest limits by Unicode characters in the editor

Backlog **#36 / ARC-45**, task ID `8404`. Priority Medium. Depends: ARC-19, ARC-20.

**Reproduce:** Fresh company login; Edit Hobby and paste 500 grinning-face emoji (U+1F600). Inspect the retained value before Save. Cancel, reload, and repeat. A direct PUT /api/me/interests with the same 500 emoji succeeds.

**Expected / source:** ARC-19 allows interests of 500 characters; backend/HANDOFF semantics explicitly count Unicode code points. The editor must accept the same valid value as its API.

**Actual:** The HTML maxLength=500 counts UTF-16 code units and silently truncates the input to 250 emoji (500 code units). The API accepts all 500 emoji. Confirmed twice with normal Playwright fill/paste behavior.

**Evidence / confirmation:** coverage-results.json: emoji input 1/2 → {codeUnits:500,codePoints:250,max:500}; browser-results.json: emoji interests → 200 with 500 emoji. Screenshot emoji-limit-1.png. Board attachment IDs: 40796.

**Fix-card acceptance:** Enforce the 500-code-point limit in interest editing without splitting surrogate pairs. Add a regression that pastes and saves 500 emoji intact, and verifies the defined handling of 501; it must fail before the fix.

## Questions for the human

- Confirm deployed staging/prod key custody and per-environment R2 secrets, then run the ARC-10 restore drill. This isolated app audit cannot establish backup recoverability or production TLS/Basic Auth behavior.
- Define whether “case-insensitive” colleague search must include non-ASCII case folding (for example accented Latin/Cyrillic names). ASCII matching was verified; no Unicode-case bug is asserted without a decided rule.
- Verify native notification grant/deny and delivery in the supported managed browsers/OSes. Constructor, permission timing, payload truncation and click behavior were exercised with explicit headless instrumentation.

## Requirement checklist

Each line is one checkable expected requirement compiled before execution. **PASS** means observed behavior, with named local tests for inaccessible/internal branches; it is not a production certification. **FAIL** references a confirmed finding. **NOT TESTABLE** names a boundary of this environment. Counted multi-part lines retain their source-card grouping; evidence states which branches were supplemented by tests.

| ID | Source | Expected behavior | Result | Evidence / limitation |
|---|---|---|---|---|
| R001 | ARC-09 | Nightly encrypted SQLite and uploads backups run at 03:00 UTC in both environments. | NOT TESTABLE | No deployed environments/R2 credentials or scheduled clock in this isolated audit. |
| R002 | ARC-09 | Staging uses st-arc and production uses arc, with separate human-managed credentials. | NOT TESTABLE | Human ARC-09 comment establishes the buckets; actual secret isolation cannot be inspected here. |
| R003 | ARC-09 | An empty backup ping URL disables pings without failing backups. | NOT TESTABLE | No restic/R2 backup runtime was started; no claim of backup validation. |
| R004 | ARC-09 | Production backs up before switching and aborts on backup failure. | NOT TESTABLE | No production deploy performed. |
| R005 | ARC-09 | Restore round trip preserves the database and upload files. | NOT TESTABLE | No Docker/restic restore drill performed; human ARC-10 remains outstanding. |
| R006 | ARC-12 | SQLite migrations round trip; foreign keys cascade and chat pairs are unique and ordered. | PASS | Local db package migration/cascade tests; browser admin cascade and distinct re-created user. |
| R007 | ARC-12 | Generated sqlc output is reproducible and timestamps are RFC3339 UTC. | PASS | make generate clean; API timestamps in browser-results.json are UTC RFC3339. |
| R008 | ARC-13 | Mock OIDC supports queued identities including email verification and hosted domain. | PASS | Independent verified, foreign-domain and unverified-email browser logins. |
| R009 | ARC-13 | Test helpers use isolated real databases and authenticated clients. | PASS | make test: testutil package with isolated real SQLite; independent audit used fresh E2E databases. |
| R010 | ARC-14 | Shell uses blue #3b5998 header, #edeff4 panels, #d8dfea borders, Tahoma/Verdana 12px, radius 2px and no shadows. | FAIL | F01: system font 16px; blocked generated theme styles. |
| R011 | ARC-14 | Content is centered and limited to 980px. | PASS | Measured desktop content x=150, width=980 at 1280px. |
| R012 | ARC-14 | Navigation exposes own profile, messenger, logout and admin only for admins. | PASS | Normal/admin browser contexts show the correct links; mobile clipping is F05. |
| R013 | ARC-14 | Protected deep links redirect to login and preserve the requested path. | PASS | Logged-out /messenger/999 becomes /login?next=%2Fmessenger%2F999. |
| R014 | ARC-14 | API 401 redirects to login; other errors show friendly notifications. | PASS | Guest API 401; simulated read/write failures render friendly notices; local API-client tests cover redirect mapping. |
| R015 | ARC-15 | Company verified Google identity signs in and creates a profile. | PASS | Independent Audit Alex/Beth, Sample Alex/Beth and Coverage User logins. |
| R016 | ARC-15 | Foreign hosted domain and unverified email are rejected with friendly login errors. | PASS | Foreign and unverified logins return the exact specified error pages. |
| R017 | ARC-15 | Bad state/nonce is rejected and external next URLs cannot redirect off site. | PASS | Bad-state callback returns 302 /login?error=state; both external next values stay local; nonce branch supplemented by auth tests. |
| R018 | ARC-15 | Repeated sign-in updates identity without duplicating the profile. | PASS | Repeated audit logins retain the same user ID; auth package verifies name updates. |
| R019 | ARC-15 | Google avatar is processed through upload; download failure does not block login. | NOT TESTABLE | No real Google picture download/remote avatar failure was driven; auth tests cover these branches. |
| R020 | ARC-16 | Session cookie is HttpOnly and SameSite=Lax; Secure outside dev; absolute 30 days and idle 7 days. | PASS | Browser cookie is HttpOnly, SameSite=Lax and non-Secure in dev; session package tests cover non-dev Secure and lifetime settings. |
| R021 | ARC-16 | Expired and deleted-user sessions return 401. | PASS | Expired SQLite session returns 401; admin-deleted Beth session returns 401. |
| R022 | ARC-16 | Logout destroys the session and clears protected UI and cache. | PASS | Final-probes logout API 401 and protected route returns to login; auth/client tests cover cache clearing. |
| R023 | ARC-16 | Mutations reject a foreign Origin and missing Origin without same-origin Fetch Metadata. | PASS | Foreign/missing Origin 403; explicit matching Origin and same-origin Fetch Metadata 200. |
| R024 | ARC-16 | GET /api/me exposes only the signed-in identity and case-insensitive admin membership. | PASS | Browser identities and admin stats access; case-insensitive membership supplemented by config/auth tests. |
| R025 | ARC-17 | Login shows ARC, the specified tagline and one Google link. | PASS | Login text captured after foreign/unverified redirects; one Google sign-in link. |
| R026 | ARC-17 | Login maps domain, unverified, state and provider errors to readable text. | PASS | All four login error URLs exercised independently. |
| R027 | ARC-18 | JPEG, PNG, WebP and first GIF frame are accepted by magic bytes. | PASS | Browser canvas PNG/JPEG/WebP, real GIF upload and accepted processed image dimensions. |
| R028 | ARC-18 | PDF/SVG, corrupt images, over 10MB and over 8000x8000 images are rejected. | FAIL | F03: truncated PNG returns 500 instead of validation error; SVG/over-10MB/8001px inputs rejected appropriately. |
| R029 | ARC-18 | Avatar is center cropped 512x512; photos fit 1600px; drawings are PNG at most 800x400. | PASS | Measured avatar 512x512 and photo 1600x800; valid drawing upload; drawing bounds supplemented by upload tests. |
| R030 | ARC-18 | Re-encoding strips EXIF/GPS and applies orientation. | PASS | Local upload fixture tests verify GPS stripping and EXIF rotation; not independently photographed in browser. |
| R031 | ARC-18 | Uploads use random paths and deletion removes stored files. | PASS | Random URLs sampled; old avatar HTTP 404 and photo deletion; file cleanup supplemented by upload/profile tests. |
| R032 | ARC-19 | Profile returns name, email, avatar, four interests, photos and correct isMe. | PASS | Profile initial/other/reborn response bodies captured. |
| R033 | ARC-19 | Interest fields trim whitespace and reject over 500 Unicode characters. | PASS | 501 emoji rejected with 400 too_long; 500 accepted by API; whitespace trimmed. Editor mismatch is F07. |
| R034 | ARC-19 | Name/email cannot be written through profile editing. | PASS | No name/email editing controls; profile API tests verify immutable identity fields. |
| R035 | ARC-19 | Avatar replacement removes the previous upload. | PASS | Two avatar uploads; first URL returns 404 after replacement. |
| R036 | ARC-19 | Five photos are accepted; the sixth returns 409 photo_limit. | PASS | Five independent uploads 201, sixth 409 photo_limit. |
| R037 | ARC-19 | Deleting another users photo returns 404; own deletion removes the file. | PASS | Other user photo deletion 404; owner confirmation and successful delete. |
| R038 | ARC-20 | Own profile offers avatar, interest and photo controls; other profiles do not. | PASS | Own controls exist; another profile has zero Edit Hobby controls. |
| R039 | ARC-20 | Profiles have no direct Start chat action. | PASS | No direct chat action on either profile. |
| R040 | ARC-20 | Interest Save/Cancel works and edits persist through reload. | FAIL | Save/Cancel/reload generally work, but F07 silently drops half of a valid emoji interest. |
| R041 | ARC-20 | Photos open in a lightbox with keyboard navigation and deletion confirmation. | PASS | Lightbox visible, ArrowRight navigation and Escape close; photo delete Cancel/Delete both exercised. |
| R042 | ARC-20 | The Add photo control hides at five and upload errors are shown. | PASS | Add photo absent at five; invalid-upload toast visible (wrong classification in F03). |
| R043 | ARC-21 | Wall accepts nonempty trimmed text up to 2000 Unicode characters and/or PNG drawings. | PASS | Empty and >2000-emoji wall requests reject; anonymous text, drawing and reply posts succeed. |
| R044 | ARC-21 | Roots paginate newest first and replies appear oldest first. | PASS | 22 seeded roots loaded with Show more; page ordering and reply ordering inspected through API. |
| R045 | ARC-21 | Replies to replies flatten to the root and cross-wall parents are rejected. | PASS | Third-level reply appears under root; cross-wall parent returns 404. |
| R046 | ARC-21 | Anonymous author is disclosed only to the wall owner, including direct API responses. | PASS | Owner response includes author, poster response author=null; no raw author_id exposed. |
| R047 | ARC-21 | Only the wall owner can delete posts; deleting a root cascades replies and drawing files. | PASS | Non-owner DELETE 404; owner root delete removes replies; file cleanup supplemented by wall tests. |
| R048 | ARC-22 | Drawing pad is 400x200, DPR scaled, white, and supports pointer strokes. | PASS | 400x200 DOM and bitmap at DPR1, white background, touch-action:none and pointer strokes; higher DPR supplemented by component tests. |
| R049 | ARC-22 | Four specified colors, two brushes, Undo and Clear work; completed strokes emit PNG. | PASS | Controls inspected, two strokes rendered, Undo retains content, Clear disables submit; colors/brush rendering supplemented by component tests. |
| R050 | ARC-23 | Wall composer supports text, drawing, anonymous warning, pending guard and reset after posting. | PASS | Draw+text posted/reset; anonymous hint; real double click produces one post twice. |
| R051 | ARC-23 | Owner sees Anonymous plus author; everyone else sees Anonymous. | PASS | Independent owner and poster views plus baseline E2E. |
| R052 | ARC-23 | Replies render nested, delete asks confirmation, and Show more loads older roots. | PASS | Nested reply in existing suite; independent owner deletion and 22-root Show more. |
| R053 | ARC-24 | Search below four characters returns [] and UI sends no search request. | PASS | API 3-char query []; network probe captured only Cove/missing requests, none for Cov. |
| R054 | ARC-24 | Search supports case-insensitive email and name-word prefixes with max ten results. | PASS | Email/name-word ASCII prefixes and no results; final-probes seeds 11 matches and returns ten. |
| R055 | ARC-24 | Search treats percent, underscore and backslash literally. | PASS | Percent/underscore/backslash queries return literal no-match results. |
| R056 | ARC-24 | Search options show avatar/name/email, debounce 250ms and navigate on selection. | PASS | Keyboard ArrowDown/Enter selects Beth and navigates; option names include email; exact debounce timer supplemented by component tests. |
| R057 | ARC-25 | Unavailable caller gets 409; no available colleague gets 404; availability toggle persists. | PASS | Unavailable 409; only-caller-available UI notice; availability persists and existing thread still sends. |
| R058 | ARC-25 | Random matching prefers new available colleagues, never self, and keeps one chat per pair. | PASS | Fresh pair, rematch same chat; new-partner preference and concurrency supplemented by chat package tests. |
| R059 | ARC-25 | Re-match reuses the chat and appends Matched again ☕ as a system message. | PASS | API rematch returns existing chat; both threads show Matched again ☕. |
| R060 | ARC-25 | Chat list is ordered by latest message and includes correct preview and unread count. | PASS | Preview/unread title captured; cross-chat ordering supplemented by chat tests; hidden acknowledgment is F02. |
| R061 | ARC-25 | History is paginated, oldest first inside pages, and restricted to members. | PASS | API limit/before responses and non-member 404; 50-message UI page grows to 57 on scroll. |
| R062 | ARC-25 | Messages trim to 1–4000 Unicode characters and work while unavailable. | PASS | Blank and 4001-emoji messages reject; unavailable member sends successfully. |
| R063 | ARC-25 | Read cursor is monotonic and non-members cannot read, send or mark another chat. | PASS | Read cursor stays at 8 after lower cursor request; outsider GET/POST/read all 404. |
| R064 | ARC-26 | Unauthenticated WebSocket upgrade is 401 and foreign Origin is rejected. | PASS | Independent raw WebSocket upgrades: no cookie 401, valid session with foreign Origin 403. |
| R065 | ARC-26 | Both participants receive chat.new and message.new with decrypted text. | PASS | New chat appears without reload; plaintext messages arrive in both threads over WS. |
| R066 | ARC-26 | Multiple tabs receive events and socket reconnect restores missed messages in order. | PASS | Second tab receives; forced close reconnects and restores three offline messages in order. |
| R067 | ARC-26 | Slow clients are dropped, pings maintain connections and shutdown closes sockets. | PASS | Realtime package tests cover slow consumer, ping and shutdown; no browser soak/load test claimed. |
| R068 | ARC-27 | Messenger has two panes on desktop and stacks on mobile. | FAIL | F01: mobile messenger flex-direction remains row and thread is compressed. |
| R069 | ARC-27 | Random button navigates to chat; unavailable tooltip and nobody-available notice work. | PASS | Start navigates; unavailable state and friendly nobody notice captured; tooltip focus supplemented by component tests. |
| R070 | ARC-27 | Message composer sends on Enter, preserves Shift+Enter and optimistically appends once. | PASS | Enter sends once, Shift+Enter retains newline, optimistic text and server response reconcile; real double Send returns one row. |
| R071 | ARC-27 | Scrolling up loads older messages while preserving position. | PASS | UI scroll to top loads 50→57 messages; anchor-position branch supplemented by component tests. |
| R072 | ARC-27 | Incoming messages update list/thread and visible thread advances read state. | FAIL | Visible reads work; F02 also marks hidden-thread messages read. |
| R073 | ARC-27 | Reconnect backs off 1s to 30s and refetches chats and open history. | PASS | Forced socket close restores missing message/history; exact 1s→30s timer bounds supplemented by realtime hook tests. |
| R074 | ARC-28 | Tab title shows summed unread count and restores on leaving messenger. | FAIL | Normal unopened chat title increments/clears; hidden open-thread messages lose unread title through F02. |
| R075 | ARC-28 | Permission is requested only by clicking Enable notifications; button hides after grant/deny. | PASS | Instrumented permission API: zero calls before click, one after, button hides on grant; deny branch covered by RTL. |
| R076 | ARC-28 | Hidden-tab events notify with partner name and first 80 characters, focusing/opening chat on click. | PASS | Instrumented hidden notification title/body=80 code points and callback focuses/opens chat. Native OS delivery is not validated. |
| R077 | ARC-29 | Built frontend E2E runs through STATIC_DIR with isolated DB and mockoidc. | PASS | make e2e and independent drivers build frontend, use STATIC_DIR, mockoidc, temp data/uploads. |
| R078 | ARC-29 | Existing suite covers profile, anonymous drawing, reply, realtime exchange and foreign-domain rejection. | PASS | Existing nine E2E tests green; independent browser/API checks repeat the key journeys. |
| R079 | ARC-30 | Go coverage is at least 80% excluding generated/wiring code; frontend line coverage at least 70%. | PASS | make lint test exit 0: Go 82.8%, frontend lines 96.3%. |
| R080 | ARC-31 | CSP remains active without breaking normal interactive controls or layout. | FAIL | F01: active restrictive CSP blocks required Mantine generated styles. |
| R081 | ARC-31 | Caddy serves nosniff, same-origin referrer, HSTS and inline upload disposition. | NOT TESTABLE | STATIC_DIR runtime is not production Caddy/TLS. HSTS/Basic Auth deployment not exercised. |
| R082 | ARC-31 | Per-user random/wall/message/upload limits are 10/20/60/20 per minute and return 429. | PASS | Independent random/wall/message saturation returns 429; separate uploader gets 20 successes then 2 rate limits; Retry-After sampled. |
| R083 | ARC-31 | Oversized route request bodies are rejected. | PASS | 40KB JSON returns 400 too_large; >10MB image returns 400 too_large. |
| R084 | ARC-31 | CI vulnerability checks and concise threat model exist. | NOT TESTABLE | No independent vulnerability scan/threat-model review; PR CI provides its own scanner result. |
| R085 | ARC-34 | Non-admin stats, preview and deletion APIs return 404. | PASS | Non-admin stats, preview and DELETE all 404; UI mismatch separately F04. |
| R086 | ARC-34 | Admin stats expose six totals and preview exposes counts only, including unknown-email zeros. | PASS | Six stats and counts-only unknown-email zero preview inspected. |
| R087 | ARC-34 | Self-delete returns 409 and deleting an unknown email returns 404. | PASS | Self-delete 409 self_delete; unknown DELETE 404. |
| R088 | ARC-34 | Deletion removes profile, interests, photos, both kinds of wall posts, sessions and complete chats. | PASS | Beth profile/session/chat/messages gone; DB chat/message counts zero and audit row present; files/authored-wall cascade supplemented by admin tests. |
| R089 | ARC-34 | Deletion audits a lowercase-email SHA-256 hash and does not log cleartext emails. | PASS | One audit row observed; exact email hash/log-redaction assertions in admin package tests. |
| R090 | ARC-35 | Admin UI renders all stats, exact-email preview, typed-email confirmation, success notice and refreshed stats. | PASS | Stats/preview, wrong-email disabled Delete, correct confirmation, success notice; refresh asserted by existing RTL tests. |
| R091 | ARC-35 | Non-admin opening /admin sees a normal 404 page. | FAIL | F04: non-admin /admin redirects to own profile, twice. |
| R092 | ARC-36 | After admin deletion partner no longer sees the chat or authored posts; deleted user can sign in to an empty profile. | PASS | Existing admin E2E plus independent fresh login yields new id, empty interests/photos. |
| R093 | ARC-37 | Missing/malformed message keys fail startup; fixed dev key permits local tools. | PASS | Local config tests for missing/malformed keys; all independent runtimes start with documented fixed dev key. |
| R094 | ARC-37 | Text and system bodies are v1 encrypted at rest with fresh nonces and chat-bound authentication. | PASS | Actual DB rows have v1 ciphertext for text/system; msgcrypt tests cover fresh nonces and chat binding. |
| R095 | ARC-37 | History, previews, POST responses and WebSocket events return decrypted messages. | PASS | REST responses/previews and live threads contain plaintext. |
| R096 | ARC-37 | Corrupted ciphertext displays [message unavailable] without failing other rows. | PASS | Corrupted latest row history/preview 200 [message unavailable], earlier rows intact; UI screenshot. |
| R097 | ARC-37 | Old-key rotation reads historical ciphertext and writes with current key. | PASS | Local msgcrypt/chat/config tests cover current/old-key behavior; not rotated in a deployed environment. |
| R098 | ARC-37 | Deployment wires separate human-managed keys and documents recovery/rotation. | NOT TESTABLE | No staging/prod secrets available. Wiring documented by ARC-37; human key storage/rotation remains unverified. |
| R099 | ARC-38 | At 1280x800 and 390x844 controls remain reachable with no clipping, overlap or unintended overflow. | FAIL | F01, F05, F06: compressed mobile messenger, clipped navigation, unbounded interest text. |
| R100 | ARC-38 | Keyboard navigation has visible focus and controls work without a mouse. | PASS | Keyboard search, lightbox arrows/Escape, focus outline auto 1px; phone clipping in F05 limits reachability. |
| R101 | ARC-38 | Empty, loading and error states remain readable; failed writes retain user input. | PASS | Independent empty/loading/profile/wall/chat/history/search/admin errors and retained failed interest draft; corrupted-file feedback is F03. |
| R102 | ARC-38 | Double submits do not duplicate a single UI action. | PASS | Real Playwright double clicks create exactly one post on both repeats and one message. Same-tick synthetic clicks excluded as non-user evidence. |
| R103 | ARC-38 | Back/forward, reload and protected deep links retain consistent page identity. | PASS | Search and thread back/forward, reload persistence, logged-out deep link and next redirects exercised. |
| R104 | ARC-38 | Long text, emoji, RTL and HTML-looking input render safely without executing markup. | FAIL | Markup is escaped and RTL/emoji render; long ASCII text overflows (F06), valid emoji input truncates (F07). |
| R105 | ARC-38 | Normal journeys have no unexpected console errors or failed network requests. | FAIL | F01: reproducible CSP console errors on normal navigation; deliberate 4xx/500 probes excluded. |

Checklist: 105 lines — 85 pass, 11 fail, 9 not testable.
