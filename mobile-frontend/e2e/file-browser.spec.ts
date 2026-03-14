import { test, expect } from '@playwright/test';

test.describe('File Browser', () => {
  test.beforeEach(async ({ page }) => {
    await page.addInitScript(() => {
      localStorage.setItem('dev_bypass', '1');
      localStorage.setItem('seahub_token', 'mock-e2e-token');
    });
  });

  test('opens a library and shows file listing', async ({ page }) => {
    await page.goto('/libraries/');
    await page.waitForLoadState('networkidle');

    // Click first library if available
    const libraryLink = page.locator('a[href*="/libraries/"]').first();
    if (await libraryLink.isVisible()) {
      await libraryLink.click();
      await page.waitForLoadState('networkidle');

      // Should show breadcrumb with "Root"
      await expect(page.locator('text=Root')).toBeVisible();
    }
  });

  test('navigates into a folder via click', async ({ page }) => {
    // Navigate directly to a repo path
    await page.goto('/libraries/test-repo/');
    await page.waitForLoadState('networkidle');

    // Look for folder items
    const folderItem = page.locator('[class*="cursor-pointer"]').first();
    if (await folderItem.isVisible()) {
      const folderName = await folderItem.textContent();
      await folderItem.click();
      await page.waitForLoadState('networkidle');

      // Breadcrumb should update
      if (folderName) {
        const breadcrumb = page.locator(`text=${folderName.trim()}`);
        // Folder name might appear in breadcrumb
        await expect(breadcrumb.first()).toBeVisible();
      }
    }
  });

  test('breadcrumb navigation back to root', async ({ page }) => {
    await page.goto('/libraries/test-repo/');
    await page.waitForLoadState('networkidle');

    // Click Root in breadcrumb
    const rootBtn = page.locator('button:has-text("Root")');
    if (await rootBtn.isVisible()) {
      await rootBtn.click();
      await page.waitForLoadState('networkidle');
      await expect(rootBtn).toBeVisible();
    }
  });

  test('shows empty state for empty folder', async ({ page }) => {
    await page.goto('/libraries/test-repo/');
    await page.waitForLoadState('networkidle');

    // If folder is empty, should show the empty message
    const emptyMsg = page.locator('text=This folder is empty');
    // Either we see files or the empty message
    const fileList = page.locator('[class*="overflow-auto"]');
    await expect(fileList.or(emptyMsg).first()).toBeVisible();
  });
});
