import path from 'node:path';
import { checkMobileShell } from './mobile-shell';
import { readFileSync } from 'node:fs';
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
let aliceProfileURL: string;

test.beforeAll(async ({ browser, playwright }) => {
  api = await playwright.request.newContext();
  aliceContext = await browser.newContext();
  bobContext = await browser.newContext();
  await aliceContext.addInitScript({ content: trackSockets });
  await bobContext.addInitScript({ content: trackSockets });
  alicePage = await aliceContext.newPage();
  bobPage = await bobContext.newPage();
  await signIn(api, alicePage, alice);
  aliceProfileURL = alicePage.url();
  await signIn(api, bobPage, bob);
});

test.afterAll(async () => {
  await api?.dispose();
  await aliceContext?.close();
  await bobContext?.close();
});

test('mobile shell controls fit and work with the keyboard before and after reload', async () => {
  await checkMobileShell(alicePage, alice.name);
});

test('truncated PNGs show a friendly validation error before and after reload', async () => {
  const buffer = readFileSync(path.join(process.cwd(), 'fixtures', 'photo.png')).subarray(0, 24);
  for (let attempt = 0; attempt < 2; attempt++) {
    if (attempt > 0) await alicePage.reload();
    await expect(alicePage.getByRole('heading', { name: 'Photos (0/5)' })).toBeVisible();
    const uploaded = alicePage.waitForResponse((response) =>
      response.url().endsWith('/api/me/photos') && response.request().method() === 'POST');
    await alicePage.getByLabel('Add a photo').setInputFiles({ name: 'truncated.png', mimeType: 'image/png', buffer });
    const response = await uploaded;
    expect(response.status()).toBe(400);
    expect(await response.json()).toEqual({
      code: 'invalid_image', message: 'That image could not be read. Try another image.',
    });
    await expect(alicePage.getByRole('alert')).toContainText('That image could not be read. Try another image.');
    await expect(alicePage.getByRole('heading', { name: 'Photos (0/5)' })).toBeVisible();
    await expect(alicePage.getByRole('button', { name: /Open photo/ })).toHaveCount(0);
  }
});

test('500-character interest words wrap within the panel after repeated saves and reloads', async () => {
  const word = 'A'.repeat(500);
  for (const viewport of [{ width: 1280, height: 800 }, { width: 390, height: 844 }]) {
    await alicePage.setViewportSize(viewport);
    for (let attempt = 0; attempt < 2; attempt++) {
      await alicePage.getByRole('button', { name: 'Edit Hobby' }).click();
      await alicePage.getByLabel('Hobby', { exact: true }).fill(word);
      await alicePage.getByRole('button', { name: 'Save', exact: true }).click();
      await expect(alicePage.getByText(word, { exact: true })).toBeVisible();
      await alicePage.reload();
      const value = alicePage.getByText(word, { exact: true });
      await expect(value).toBeVisible();
      const geometry = await value.evaluate((element) => {
        const box = element.getBoundingClientRect();
        const panel = element.closest('.mantine-Paper-root')!.getBoundingClientRect();
        return {
          height: box.height,
          lineHeight: Number.parseFloat(getComputedStyle(element).lineHeight),
          left: box.left,
          right: box.right,
          panelLeft: panel.left,
          panelRight: panel.right,
          scrollWidth: element.scrollWidth,
          clientWidth: element.clientWidth,
          documentWidth: document.documentElement.scrollWidth,
        };
      });
      expect(geometry.height).toBeGreaterThan(geometry.lineHeight);
      expect(geometry.left).toBeGreaterThanOrEqual(geometry.panelLeft);
      expect(geometry.right).toBeLessThanOrEqual(geometry.panelRight);
      expect(geometry.scrollWidth).toBeLessThanOrEqual(geometry.clientWidth);
      expect(geometry.documentWidth).toBeLessThanOrEqual(viewport.width);
      const response = await aliceContext.request.get(new URL(aliceProfileURL).pathname.replace('/u/', '/api/users/'));
      expect(response.ok()).toBe(true);
      expect((await response.json()).interests.hobby).toBe(word);
      await alicePage.getByRole('button', { name: 'Edit Hobby' }).click();
      await expect(alicePage.getByLabel('Hobby', { exact: true })).toHaveValue(word);
      await alicePage.getByRole('button', { name: 'Cancel', exact: true }).click();
    }
  }
  await alicePage.setViewportSize({ width: 1280, height: 800 });
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

test('an open background thread retains server unread counts until visibility returns', async () => {
  const chatId = Number(alicePage.url().split('/').pop());
  await waitForOpenSocket(alicePage);
  await expect(alicePage).toHaveTitle('ARC');
  const readRequests: string[] = [];
  const recordRead = (request: { url(): string }) => {
    if (request.url().endsWith(`/api/chats/${chatId}/read`)) readRequests.push(request.url());
  };
  alicePage.on('request', recordRead);
  // Headless Chromium does not reliably background pages across browser contexts.
  // Instrument the Page Visibility API explicitly, as in the ARC-38 audit.
  await alicePage.evaluate(() => {
    Object.defineProperty(document, 'hidden', { configurable: true, get: () => true });
    Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => 'hidden' });
    document.dispatchEvent(new Event('visibilitychange'));
  });
  try {
    for (const count of [1, 2]) {
      const composer = bobPage.getByRole('form', { name: 'Message composer' });
      await composer.getByLabel('Message').fill(`Background message ${count}`);
      await composer.getByRole('button', { name: 'Send' }).click();
      await expect(alicePage.getByRole('log', { name: 'Messages' })).toContainText(`Background message ${count}`);
      await expect(alicePage).toHaveTitle(`(${count}) ARC`);
      await expect(alicePage.getByLabel(`${count} unread`, { exact: true })).toBeVisible();
      const response = await aliceContext.request.get('/api/chats');
      expect(response.ok()).toBe(true);
      const chats = await response.json() as { id: number; unreadCount: number }[];
      expect(chats.find((chat) => chat.id === chatId)?.unreadCount).toBe(count);
      expect(readRequests).toEqual([]);
    }
    const acknowledged = alicePage.waitForResponse((response) =>
      response.url().endsWith(`/api/chats/${chatId}/read`) && response.ok());
    await alicePage.evaluate(() => {
      Object.defineProperty(document, 'hidden', { configurable: true, get: () => false });
      Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => 'visible' });
      document.dispatchEvent(new Event('visibilitychange'));
    });
    const response = await acknowledged;
    const cursor = await response.json() as { lastMessageId: number };
    const messagesResponse = await aliceContext.request.get(`/api/chats/${chatId}/messages`);
    const { messages } = await messagesResponse.json() as { messages: { id: number }[] };
    expect(cursor.lastMessageId).toBe(messages.at(-1)?.id);
    await expect(alicePage).toHaveTitle('ARC');
    await expect(alicePage.getByLabel(/unread/)).toHaveCount(0);
    const chatsResponse = await aliceContext.request.get('/api/chats');
    const chats = await chatsResponse.json() as { id: number; unreadCount: number }[];
    expect(chats.find((chat) => chat.id === chatId)?.unreadCount).toBe(0);
  } finally {
    alicePage.off('request', recordRead);
    await alicePage.evaluate(() => {
      Reflect.deleteProperty(document, 'hidden');
      Reflect.deleteProperty(document, 'visibilityState');
      document.dispatchEvent(new Event('visibilitychange'));
    });
  }
});

test('CSP allows the theme, shell spacing, and mobile messenger styles after reload', async () => {
  const chatURL = alicePage.url();
  const styleViolations: string[] = [];
  await alicePage.exposeFunction('recordStyleViolation', (directive: string) => {
    styleViolations.push(directive);
  });
  await alicePage.addInitScript(() => {
    document.addEventListener('securitypolicyviolation', (event) => {
      if (event.effectiveDirective.startsWith('style-src')) {
        void (window as unknown as { recordStyleViolation: (directive: string) => Promise<void> })
          .recordStyleViolation(event.effectiveDirective);
      }
    });
  });

  await alicePage.setViewportSize({ width: 1280, height: 800 });
  await alicePage.goto(aliceProfileURL);
  for (let attempt = 0; attempt < 2; attempt++) {
    await alicePage.reload();
    const heading = alicePage.getByRole('heading', { level: 1, name: alice.name });
    await expect(heading).toBeVisible();
    await expect(alicePage.locator('body')).toHaveCSS('font-family', 'Tahoma, Verdana, sans-serif');
    await expect(alicePage.locator('body')).toHaveCSS('font-size', '12px');
    const header = alicePage.locator('header');
    await expect(header).toHaveCSS('background-color', 'rgb(59, 89, 152)');
    const headerBox = await header.boundingBox();
    const headingBox = await heading.boundingBox();
    expect(headerBox).not.toBeNull();
    expect(headingBox).not.toBeNull();
    expect(headingBox!.y).toBeGreaterThanOrEqual(headerBox!.y + headerBox!.height);
    expect(styleViolations).toEqual([]);
  }

  await alicePage.setViewportSize({ width: 390, height: 844 });
  await alicePage.goto(chatURL);
  for (let attempt = 0; attempt < 2; attempt++) {
    await alicePage.reload();
    await expect(alicePage.getByRole('heading', { level: 2, name: bob.name })).toBeVisible();
    // This is the Flex containing both the chat list and the message pane.
    const panes = alicePage.locator('main .mantine-Flex-root').filter({
      has: alicePage.getByRole('form', { name: 'Message composer' }),
    });
    await expect(panes).toHaveCSS('flex-direction', 'column');
    await expect(alicePage.getByRole('form', { name: 'Message composer' })).toBeVisible();
    expect(styleViolations).toEqual([]);
  }
  await alicePage.setViewportSize({ width: 1280, height: 800 });
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
