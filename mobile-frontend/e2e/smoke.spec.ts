import { test, expect } from '@playwright/test';

test.describe('Smoke Tests', () => {
  test.beforeEach(async ({ page }) => {
    // Set dev bypass to skip authentication
    await page.addInitScript(() => {
      localStorage.setItem('dev_bypass', '1');
    });
  });

  test('navigates to / and redirects to /libraries/', async ({ page }) => {
    await page.goto('/');
    await expect(page).toHaveURL(/\/(libraries|login)/);
  });

  test('bottom nav is visible with tabs', async ({ page }) => {
    await page.goto('/libraries/');
    const nav = page.locator('nav');
    await expect(nav).toBeVisible();

    // Should have navigation links
    const links = nav.locator('a');
    await expect(links).toHaveCount(5);
  });

  test('tab navigation works', async ({ page }) => {
    await page.goto('/libraries/');

    // Click starred tab
    await page.click('a[href*="starred"]');
    await expect(page).toHaveURL(/starred/);

    // Click activity tab
    await page.click('a[href*="activity"]');
    await expect(page).toHaveURL(/activity/);

    // Click back to libraries
    await page.click('a[href*="libraries"]');
    await expect(page).toHaveURL(/libraries/);
  });

  test('each page loads without JavaScript errors', async ({ page }) => {
    const errors: string[] = [];
    page.on('pageerror', (err) => errors.push(err.message));

    const pages = ['/libraries/', '/starred/', '/activity/', '/shared/', '/more/'];
    for (const path of pages) {
      await page.goto(path);
      await page.waitForLoadState('networkidle');
    }

    // Filter out non-critical errors
    const criticalErrors = errors.filter(e =>
      !e.includes('ResizeObserver') && !e.includes('ServiceWorker')
    );
    expect(criticalErrors).toHaveLength(0);
  });

  test('no Vite preamble or React plugin errors on any page', async ({ page }) => {
    const errors: string[] = [];
    page.on('pageerror', (err) => errors.push(err.message));

    const pages = ['/libraries/', '/starred/', '/more/', '/login/'];
    for (const path of pages) {
      await page.goto(path);
      await page.waitForLoadState('domcontentloaded');
    }

    const preambleErrors = errors.filter(e =>
      e.includes('preamble') || e.includes('@vitejs/plugin-react')
    );
    expect(preambleErrors).toHaveLength(0);
  });

  test('static assets (logo) load without 404', async ({ page }) => {
    const notFound: string[] = [];
    page.on('response', (res) => {
      if (res.status() === 404 && res.url().includes('logo')) {
        notFound.push(res.url());
      }
    });

    await page.goto('/login/');
    await page.waitForLoadState('networkidle');

    expect(notFound).toHaveLength(0);
  });
});
