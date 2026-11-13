import { expect, type Page } from '@playwright/test';

/** Exercise the real shell at the audit viewport, including a cold reload. */
export async function checkMobileShell(page: Page, name: string, isAdmin = false) {
  const originalURL = page.url();
  const viewport = page.viewportSize();
  const profileURL = page.url();
  await page.setViewportSize({ width: 390, height: 844 });
  try {
    for (const path of [profileURL, '/messenger', ...(isAdmin ? ['/admin'] : [])]) {
      await page.goto(path);
      for (const reload of [false, true]) {
        if (reload) await page.reload();
        const header = page.locator('header');
        const search = header.getByRole('combobox', { name: 'Search colleagues' });
        await expect(search).toBeVisible();
        const links = [
          header.getByRole('link', { name: 'ARC', exact: true }),
          header.getByRole('link', { name: 'My profile' }),
          header.getByRole('link', { name: 'Messenger' }),
          ...(isAdmin ? [header.getByRole('link', { name: 'Admin', exact: true })] : []),
          header.getByRole('button', { name: 'Log out' }),
          search,
        ];
        for (const control of links) {
          const box = await control.boundingBox();
          const headerBox = await header.boundingBox();
          expect(box).not.toBeNull();
          expect(box!.x).toBeGreaterThanOrEqual(0);
          expect(box!.x + box!.width).toBeLessThanOrEqual(390);
          expect(box!.y).toBeGreaterThanOrEqual(headerBox!.y);
          expect(box!.y + box!.height).toBeLessThanOrEqual(headerBox!.y + headerBox!.height);
        }
        expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(390);
        const headerBox = await header.boundingBox();
        const contentBox = await page.getByTestId('content').boundingBox();
        expect(contentBox!.y).toBeGreaterThanOrEqual(headerBox!.y + headerBox!.height);
        await links[0]!.focus();
        for (const control of links.slice(1)) {
          await page.keyboard.press('Tab');
          await expect(control).toBeFocused();
        }
        await search.fill(name.slice(0, 4));
        const option = page.getByRole('option', { name: new RegExp(name) });
        await expect(option).toBeVisible();
        const optionBox = await option.boundingBox();
        expect(optionBox!.x).toBeGreaterThanOrEqual(0);
        expect(optionBox!.x + optionBox!.width).toBeLessThanOrEqual(390);
        await page.keyboard.press('ArrowDown');
        await page.keyboard.press('Enter');
        await expect(page.getByRole('heading', { level: 1, name })).toBeVisible();
        // Restore the route being checked before testing its reload.
        await page.goto(path);
      }
    }
  } finally {
    if (viewport) await page.setViewportSize(viewport);
    await page.goto(originalURL);
  }
}
