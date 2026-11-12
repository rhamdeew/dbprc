import path from 'node:path';
import { expect, test, type APIRequestContext, type BrowserContext, type Page } from '@playwright/test';

const mockOrigin = process.env.E2E_MOCK_ORIGIN ?? 'http://127.0.0.1:9999';

const alice = { email: 'alice.arc@example.com', name: 'Alice Arc', hd: 'example.com' };
const bob = { email: 'bob.arc@example.com', name: 'Bob Arc', hd: 'example.com' };

type QueuedUser = { email: string; name: string; hd: string };

// Evaluated in the page before the app loads, so the test can see socket.readyState.
const trackSockets = `(() => {
  const Original = window.WebSocket;
  const sockets = [];
  window.__e2eSockets = sockets;
  function Wrapped(url, protocols) {
    const socket = protocols === undefined ? new Original(url) : new Original(url, protocols);
    sockets.push(socket);
    return socket;
  }
  Wrapped.prototype = Original.prototype;
  Wrapped.CONNECTING = Original.CONNECTING;
  Wrapped.OPEN = Original.OPEN;
  Wrapped.CLOSING = Original.CLOSING;
  Wrapped.CLOSED = Original.CLOSED;
  window.WebSocket = Wrapped;
})();`;

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

async function drawStroke(page: Page, pad: ReturnType<Page['getByRole']>) {
  await pad.scrollIntoViewIfNeeded();
  const box = await pad.boundingBox();
  if (!box) throw new Error('drawing pad has no box');
  await page.mouse.move(box.x + 24, box.y + 30);
  await page.mouse.down();
  await page.mouse.move(box.x + 140, box.y + 80, { steps: 12 });
  await page.mouse.up();
}

async function waitForOpenSocket(page: Page) {
  await page.waitForFunction(() => {
    const list = (window as unknown as { __e2eSockets?: WebSocket[] }).__e2eSockets;
    return list?.some((socket) => socket.readyState === WebSocket.OPEN && socket.url.endsWith('/ws')) ?? false;
  });
}

test.describe.configure({ mode: 'serial' });

let aliceContext: BrowserContext;
let bobContext: BrowserContext;
let alicePage: Page;
let bobPage: Page;
let api: APIRequestContext;

test.beforeAll(async ({ browser, playwright }) => {
  api = await playwright.request.newContext();
  aliceContext = await browser.newContext();
  bobContext = await browser.newContext();
  await aliceContext.addInitScript({ content: trackSockets });
  await bobContext.addInitScript({ content: trackSockets });
  alicePage = await aliceContext.newPage();
  bobPage = await bobContext.newPage();
  await signIn(api, alicePage, alice);
  await signIn(api, bobPage, bob);
});

test.afterAll(async () => {
  await api?.dispose();
  await aliceContext?.close();
  await bobContext?.close();
});

test('Alice edits an interest, uploads a photo, and opens the lightbox', async () => {
  await alicePage.getByRole('button', { name: 'Edit Hobby' }).click();
  await alicePage.getByLabel('Hobby').fill('Chess');
  await alicePage.getByRole('button', { name: 'Save' }).click();
  await expect(alicePage.getByText('Chess', { exact: true })).toBeVisible();

  await alicePage.getByLabel('Add a photo').setInputFiles(path.join(process.cwd(), 'fixtures', 'photo.png'));
  await alicePage.getByRole('button', { name: 'Open photo 1' }).click();

  const slide = alicePage.getByRole('dialog', { name: 'Lightbox' }).getByRole('img', { name: 'Photo 1 of 1' });
  await expect(slide).toBeVisible();
  await expect.poll(async () => slide.evaluate((img: HTMLImageElement) => img.naturalWidth)).toBeGreaterThan(0);
  await alicePage.getByRole('button', { name: 'Close' }).click();
  await expect(alicePage.getByRole('dialog', { name: 'Lightbox' })).toBeHidden();
});

test('Bob posts an anonymous drawing on Alice\'s wall and Alice deletes it', async () => {
  // Mantine Autocomplete exposes a combobox. The visible label is the placeholder.
  const search = bobPage.getByRole('search').getByRole('combobox');
  await search.fill('alic');
  await bobPage.getByRole('option', { name: /Alice Arc/ }).click();
  await expect(bobPage.getByRole('heading', { level: 1, name: 'Alice Arc' })).toBeVisible();

  const composer = bobPage.getByRole('form', { name: 'Wall composer' });
  await composer.getByRole('checkbox', { name: 'Draw' }).check();
  await drawStroke(bobPage, composer.getByRole('application', { name: 'Drawing pad' }));
  await expect(composer.getByRole('button', { name: 'Post' })).toBeEnabled();
  await composer.getByLabel('Write on this wall').fill('A doodle for Alice');
  await composer.getByRole('checkbox', { name: 'Post anonymously' }).check();
  await expect(composer.getByText("The owner of this wall will still see it's you")).toBeVisible();
  await composer.getByRole('button', { name: 'Post' }).click();

  await expect(bobPage.getByText('Anonymous', { exact: true })).toBeVisible();
  await expect(bobPage.getByText('Anonymous · Bob Arc')).toHaveCount(0);
  const bobDrawing = bobPage.getByRole('img', { name: 'Drawing' });
  await expect(bobDrawing).toBeVisible();
  await expect.poll(async () => bobDrawing.evaluate((img: HTMLImageElement) => img.naturalWidth)).toBeGreaterThan(0);

  await alicePage.reload();
  await expect(alicePage.getByText('Anonymous · Bob Arc')).toBeVisible();
  await expect(alicePage.getByRole('img', { name: 'Drawing' })).toBeVisible();
  await alicePage.getByRole('button', { name: 'Delete', exact: true }).click();
  await alicePage.getByRole('dialog', { name: 'Delete this post?' }).getByRole('button', { name: 'Delete' }).click();
  await expect(alicePage.getByText('A doodle for Alice')).toHaveCount(0);
  await expect(alicePage.getByText('Anonymous · Bob Arc')).toHaveCount(0);
});

test('Bob\'s reply to Alice is nested', async () => {
  const wall = alicePage.getByRole('form', { name: 'Wall composer' });
  await wall.getByLabel('Write on this wall').fill('Hello from Alice');
  await wall.getByRole('button', { name: 'Post' }).click();
  await expect(wall.getByLabel('Write on this wall')).toHaveValue('');
  await expect(alicePage.getByText('Hello from Alice')).toBeVisible();

  await bobPage.reload();
  const thread = bobPage
    .locator('div')
    .filter({ hasText: 'Hello from Alice' })
    .filter({ has: bobPage.getByRole('button', { name: 'Reply' }) })
    .last();
  await thread.getByRole('button', { name: 'Reply' }).click();
  const reply = thread.getByRole('form', { name: 'Reply composer' });
  await reply.getByLabel('Write a reply').fill('Thanks Alice');
  await reply.getByRole('button', { name: 'Post' }).click();
  await expect(reply).toHaveCount(0);
  await expect(bobPage.getByText('Thanks Alice')).toBeVisible();

  const nested = await bobPage.getByText('Thanks Alice').evaluate((el) => {
    let node: HTMLElement | null = el.parentElement;
    while (node) {
      if (getComputedStyle(node).borderLeftWidth === '2px') return true;
      node = node.parentElement;
    }
    return false;
  });
  expect(nested).toBe(true);

  await alicePage.reload();
  await expect(alicePage.getByText('Thanks Alice')).toBeVisible();
  const nestedForAlice = await alicePage.getByText('Thanks Alice').evaluate((el) => {
    let node: HTMLElement | null = el.parentElement;
    while (node) {
      if (getComputedStyle(node).borderLeftWidth === '2px') return true;
      node = node.parentElement;
    }
    return false;
  });
  expect(nestedForAlice).toBe(true);
});

test('a random chat shows up live and both people can write', async () => {
  await bobPage.goto('/messenger');
  await expect(bobPage.getByRole('heading', { level: 1, name: 'Messenger' })).toBeVisible();
  await expect(bobPage.getByText('No chats yet. Start a random one.')).toBeVisible();
  await waitForOpenSocket(bobPage);

  await alicePage.goto('/messenger');
  await alicePage.getByRole('button', { name: 'Start random chat ☕' }).click();
  await alicePage.waitForURL(/\/messenger\/\d+$/);
  await expect(alicePage.getByRole('heading', { level: 2, name: 'Bob Arc' })).toBeVisible();
  await expect(alicePage.getByText('No messages yet.')).toBeVisible();

  const chatLink = bobPage.getByRole('link', { name: /Alice Arc/ });
  await expect(chatLink).toBeVisible();
  await chatLink.click();
  await expect(bobPage.getByRole('heading', { level: 2, name: 'Alice Arc' })).toBeVisible();
  await expect(bobPage.getByText('No messages yet.')).toBeVisible();

  const aliceComposer = alicePage.getByRole('form', { name: 'Message composer' });
  await aliceComposer.getByLabel('Message').fill('Hello from Alice');
  await aliceComposer.getByRole('button', { name: 'Send' }).click();
  await expect(bobPage.getByRole('log', { name: 'Messages' })).toContainText('Hello from Alice');

  const bobComposer = bobPage.getByRole('form', { name: 'Message composer' });
  await bobComposer.getByLabel('Message').fill('Hello from Bob');
  await bobComposer.getByRole('button', { name: 'Send' }).click();
  await expect(alicePage.getByRole('log', { name: 'Messages' })).toContainText('Hello from Bob');
});

test('a hosted domain outside the company shows the login error', async ({ browser }) => {
  const context = await browser.newContext();
  const page = await context.newPage();
  try {
    await queueUser(api, { email: alice.email, name: alice.name, hd: 'other.com' });
    await page.goto('/login');
    await page.getByRole('link', { name: 'Sign in with Google' }).click();
    await page.waitForURL(/\/login\?error=domain/);
    await expect(page.getByRole('alert')).toHaveText('Use your company Google account');
  } finally {
    await context.close();
  }
});
