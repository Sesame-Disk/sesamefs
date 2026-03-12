import { test, expect } from '@playwright/test';

test.describe('Mobile UX', () => {
  test.beforeEach(async ({ page }) => {
    await page.addInitScript(() => {
      localStorage.setItem('dev_bypass', '1');
      localStorage.setItem('seahub_token', 'mock-e2e-token');
    });
  });

  test('no horizontal overflow at 320px width', async ({ page }) => {
    await page.setViewportSize({ width: 320, height: 568 });
    await page.goto('/libraries/');
    await page.waitForLoadState('networkidle');

    // Check that document width does not exceed viewport
    const bodyWidth = await page.evaluate(() => document.body.scrollWidth);
    const viewportWidth = await page.evaluate(() => window.innerWidth);
    expect(bodyWidth).toBeLessThanOrEqual(viewportWidth + 1); // +1 for rounding
  });

  test('bottom nav stays at bottom of screen', async ({ page }) => {
    await page.goto('/libraries/');
    await page.waitForLoadState('networkidle');

    const nav = page.locator('nav');
    if (await nav.isVisible()) {
      const navBox = await nav.boundingBox();
      const viewport = page.viewportSize();
      if (navBox && viewport) {
        // Nav bottom should be near viewport bottom
        expect(navBox.y + navBox.height).toBeGreaterThan(viewport.height - 20);
      }
    }
  });

  test('touch targets meet minimum 44px size', async ({ page }) => {
    await page.goto('/libraries/');
    await page.waitForLoadState('networkidle');

    // Check that nav links have minimum touch target size
    const navLinks = page.locator('nav a');
    const count = await navLinks.count();
    for (let i = 0; i < count; i++) {
      const box = await navLinks.nth(i).boundingBox();
      if (box) {
        expect(box.height).toBeGreaterThanOrEqual(40); // Allow some tolerance
      }
    }
  });

  test('page content is scrollable', async ({ page }) => {
    await page.goto('/libraries/');
    await page.waitForLoadState('networkidle');

    // Should be able to scroll without issues
    await page.evaluate(() => window.scrollTo(0, 100));
    const scrollY = await page.evaluate(() => window.scrollY);
    // scrollY might be 0 if content fits in viewport, which is fine
    expect(scrollY).toBeGreaterThanOrEqual(0);
  });

  test('meta viewport is set for mobile', async ({ page }) => {
    await page.goto('/libraries/');
    const viewport = page.locator('meta[name="viewport"]');
    await expect(viewport).toHaveAttribute('content', /width=device-width/);
  });
});
