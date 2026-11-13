import { expect, test, type APIRequestContext, type BrowserContext, type Page } from '@playwright/test';

import { checkMobileShell } from './mobile-shell';

const mockOrigin = process.env.E2E_MOCK_ORIGIN ?? 'http://127.0.0.1:9999';

// admin.email is ADMIN_EMAILS in scripts/e2e.sh (same address as .env.dev.example).
// Nora and Owen are not app.spec.ts's Alice and Bob. That file's random chat
// must still pair those two, so this file turns its users unavailable at the end.
const nora = { email: 'nora.arc36@example.com', name: 'Nora North', hd: 'example.com' };
const owen = { email: 'owen.arc36@example.com', name: 'Owen East', hd: 'example.com' };
const admin = { email: 'admin@example.com', name: 'Arc Admin', hd: 'example.com' };

const noraPost = 'Nora keeps this';
const owenPost = 'Note from Owen';

type QueuedUser = { email: string; name: string; hd: string };

async function queueUser(request: APIRequestContext, user: QueuedUser) {
  const response = await request.post(`${mockOrigin}/_mock/next-user`, { data: user });
  if (!response.ok()) {
    throw new Error(`next-user ${response.status()} ${await response.text()}`);
  }
}

async function signIn(request: APIRequestContext, page: Page, user: QueuedUser) {
  await queueUser(request, user);
  await page.goto('/login');
  await page.getByRole('link', { name: 'Sign in with Google' }).click();
  await page.waitForURL(/\/u\/\d+$/);
  await expect(page.getByRole('heading', { level: 1, name: user.name })).toBeVisible();
}

function profilePath(page: Page): string {
  return new URL(page.url()).pathname;
}

async function setUnavailable(page: Page | undefined) {
  if (!page || page.isClosed()) return;
  await page.evaluate(async () => {
    const response = await fetch('/api/me/availability', {
      method: 'PUT',
      credentials: 'include',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify({ available: false }),
    });
    if (!response.ok && response.status !== 401) {
      throw new Error(`availability ${response.status}`);
    }
  });
}

async function postOnWall(page: Page, body: string) {
  const composer = page.getByRole('form', { name: 'Wall composer' });
  await composer.getByLabel('Write on this wall').fill(body);
  await composer.getByRole('button', { name: 'Post' }).click();
  await expect(page.getByText(body)).toBeVisible();
}

test.describe.configure({ mode: 'serial' });

let noraContext: BrowserContext | undefined;
let owenContext: BrowserContext | undefined;
let adminContext: BrowserContext | undefined;
let noraPage: Page | undefined;
let owenPage: Page | undefined;
let adminPage: Page | undefined;
let api: APIRequestContext | undefined;
let noraProfile = '';
let owenProfile = '';

test.beforeAll(async ({ browser, playwright }) => {
  api = await playwright.request.newContext();
  noraContext = await browser.newContext();
  owenContext = await browser.newContext();
  adminContext = await browser.newContext();
  noraPage = await noraContext.newPage();
  owenPage = await owenContext.newPage();
  adminPage = await adminContext.newPage();

  // Sign the admin in only after the pair exists, so random match cannot pick them.
  await signIn(api, noraPage, nora);
  await signIn(api, owenPage, owen);
  noraProfile = profilePath(noraPage);
  owenProfile = profilePath(owenPage);

  await postOnWall(noraPage, noraPost);

  const search = owenPage.getByRole('search').getByRole('combobox');
  await search.fill('nora');
  await owenPage.getByRole('option', { name: /Nora North/ }).click();
  await expect(owenPage.getByRole('heading', { level: 1, name: 'Nora North' })).toBeVisible();
  await postOnWall(owenPage, owenPost);

  await owenPage.goto('/messenger');
  await expect(owenPage.getByRole('heading', { level: 1, name: 'Messenger' })).toBeVisible();
  await expect(owenPage.getByText('No chats yet. Start a random one.')).toBeVisible();

  await noraPage.goto('/messenger');
  await noraPage.getByRole('button', { name: 'Start random chat ☕' }).click();
  await noraPage.waitForURL(/\/messenger\/\d+$/);
  await expect(noraPage.getByRole('heading', { level: 2, name: 'Owen East' })).toBeVisible();
  await expect(owenPage.getByRole('link', { name: /Nora North/ })).toBeVisible();
});

test.afterAll(async () => {
  await setUnavailable(noraPage).catch(() => undefined);
  await setUnavailable(adminPage).catch(() => undefined);
  await api?.dispose();
  await noraContext?.close();
  await owenContext?.close();
  await adminContext?.close();
});

test('an admin deletes Owen after the chat and the wall posts', async () => {
  await signIn(api!, adminPage!, admin);
  await checkMobileShell(adminPage!, admin.name, true);
  await adminPage!.goto('/admin');
  await expect(adminPage!.getByRole('heading', { level: 1, name: 'Admin' })).toBeVisible();
  await expect(adminPage!.getByRole('link', { name: 'Admin' })).toBeVisible();

  const remove = adminPage!.getByRole('region', { name: 'Remove profile' });
  await remove.getByRole('textbox', { name: 'Email' }).fill(owen.email);
  await remove.getByRole('button', { name: 'Preview' }).click();
  await expect(remove.getByText('No profile with that email.')).toHaveCount(0);
  await expect(remove.getByRole('group', { name: 'Chats' })).toContainText('1');
  await expect(remove.getByRole('group', { name: 'Posts they wrote' })).not.toContainText('0');

  await remove.getByRole('button', { name: 'Remove profile' }).click();
  const dialog = adminPage!.getByRole('dialog', { name: 'Remove this profile?' });
  await dialog.getByRole('textbox', { name: 'Confirm email' }).fill(owen.email);
  await dialog.getByRole('button', { name: 'Delete' }).click();
  await expect(adminPage!.getByText('Profile removed.')).toBeVisible();
  await expect(dialog).toBeHidden();
});

test("Nora's chat list and wall no longer include Owen", async () => {
  await noraPage!.goto('/messenger');
  await expect(noraPage!.getByRole('heading', { level: 1, name: 'Messenger' })).toBeVisible();
  await expect(noraPage!.getByRole('link', { name: /Owen East/ })).toHaveCount(0);
  await expect(noraPage!.getByText('No chats yet. Start a random one.')).toBeVisible();

  await noraPage!.goto(noraProfile);
  await expect(noraPage!.getByRole('heading', { level: 1, name: 'Nora North' })).toBeVisible();
  await expect(noraPage!.getByText(owenPost)).toHaveCount(0);
  await expect(noraPage!.getByText(noraPost)).toBeVisible();
});

test('Owen can sign in again and gets an empty profile', async () => {
  // A new page avoids the deleted session's 401, which assigns /login?next=...
  // and would interrupt a navigation started from the old messenger tab.
  const fresh = await owenContext!.newPage();
  try {
    await signIn(api!, fresh, owen);

    const freshPath = profilePath(fresh);
    expect(freshPath).not.toBe(owenProfile);
    await expect(fresh.getByRole('heading', { name: 'Photos (0/5)' })).toBeVisible();
    await expect(fresh.getByText('No photos yet.')).toBeVisible();
    await expect(fresh.getByText('No posts yet.')).toBeVisible();
    await expect(fresh.getByText('Not set', { exact: true })).toHaveCount(4);

    await fresh.goto(owenProfile);
    await expect(fresh.getByRole('heading', { level: 1, name: 'Profile' })).toBeVisible();
    // The missing-profile request also raises the same sentence in a toast.
    await expect(fresh.getByTestId('content').getByText('Profile not found.')).toBeVisible();
  } finally {
    await setUnavailable(fresh).catch(() => undefined);
    await fresh.close();
  }
});

test('a non-admin sees not found at /admin and unknown routes, including after reload', async () => {
  for (const path of ['/admin', '/unknown-route']) {
    await noraPage!.goto(path);
    for (const reload of [false, true]) {
      if (reload) await noraPage!.reload();
      await expect(noraPage!.getByRole('heading', { level: 1, name: '404 · Page not found' })).toBeVisible();
      expect(profilePath(noraPage!)).toBe(path);
      await expect(noraPage!.getByText('We couldn’t find that page.')).toBeVisible();
      await expect(noraPage!.getByRole('heading', { name: 'Nora North' })).toHaveCount(0);
      await expect(noraPage!.getByRole('heading', { name: 'Admin' })).toHaveCount(0);
      await expect(noraPage!.getByRole('link', { name: 'Admin' })).toHaveCount(0);
      await expect(noraPage!.getByRole('region', { name: 'Stats' })).toHaveCount(0);
      await expect(noraPage!.getByRole('region', { name: 'Remove profile' })).toHaveCount(0);
    }
  }

  const denied = await noraPage!.request.get('/api/admin/stats');
  expect(denied.status()).toBe(404);
  expect(await denied.json()).toEqual({ code: 'not_found', message: 'Not found.' });
});
