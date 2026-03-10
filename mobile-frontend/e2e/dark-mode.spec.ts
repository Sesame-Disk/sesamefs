import { test, expect } from '@playwright/test';

test.describe('Dark Mode', () => {
  test('toggles dark mode from settings page', async ({ page }) => {
    await page.goto('/more/');

    // Should default to system (light)
    const html = page.locator('html');
    await expect(html).not.toHaveClass(/dark/);

    // Click Dark option
    await page.getByText('Dark').click();
    await expect(html).toHaveClass(/dark/);

    // Click Light option
    await page.getByText('Light').click();
    await expect(html).not.toHaveClass(/dark/);
  });

  test('persists theme preference across navigation', async ({ page }) => {
    await page.goto('/more/');

    // Set dark mode
    await page.getByText('Dark').click();
    await expect(page.locator('html')).toHaveClass(/dark/);

    // Navigate away and back
    await page.goto('/libraries/');
    await expect(page.locator('html')).toHaveClass(/dark/);
  });

  test('respects system preference detection', async ({ page }) => {
    // Emulate dark color scheme
    await page.emulateMedia({ colorScheme: 'dark' });
    await page.goto('/more/');

    // System mode should detect dark
    const html = page.locator('html');
    await expect(html).toHaveClass(/dark/);
  });

  test('light vs dark visual difference', async ({ page }) => {
    await page.goto('/more/');

    // Take light screenshot
    await page.getByText('Light').click();
    const lightScreenshot = await page.screenshot();

    // Take dark screenshot
    await page.getByText('Dark').click();
    const darkScreenshot = await page.screenshot();

    // Screenshots should differ
    expect(Buffer.compare(lightScreenshot, darkScreenshot)).not.toBe(0);
  });
});
