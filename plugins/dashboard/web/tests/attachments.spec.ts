import { test, expect } from '@playwright/test';

test.beforeEach(async ({ request }) => {
  await request.post('/api/action', { data: { kind: 'mock-reset' } });
});

test('附件设置默认500MB，保存后刷新保持且说明当前底层限制', async ({ page }) => {
  await page.route('**/api/schema', async route => {
    await new Promise(resolve => setTimeout(resolve, 300));
    await route.continue();
  });
  await page.goto('/#settings?tab=attachments');
  const limit = page.getByRole('spinbutton', { name: /单个文件大小上限/ });
  await expect(limit).toHaveValue('500');
  await expect(page.getByText(/当前 core 发送链路最多 32 MB/)).toBeVisible();
  await limit.fill('100');
  await page.getByRole('button', { name: '保存设置', exact: true }).click();
  await expect(page.getByRole('status')).toContainText('设置已保存');
  await page.reload();
  await expect(limit).toHaveValue('100');
  await page.setViewportSize({ width: 390, height: 844 });
  await page.reload();
  await expect(limit).toHaveValue('100');
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.screenshot({ path: '/tmp/weclay-attachment-settings.png', fullPage: true });
});

test('17MiB普通文件走二进制上传，跳过FileReader与JSON媒体', async ({ page, request }) => {
  await page.addInitScript(() => {
    FileReader.prototype.readAsDataURL = () => { throw new Error('大附件不应经过FileReader'); };
  });
  let uploaded = 0;
  page.on('request', (r) => {
    if (r.url().includes('/api/attachments/send?')) {
      uploaded++;
      expect(r.headers()['content-type']).toBe('application/octet-stream');
    }
  });
  await page.goto('/#wechat?target=mock-contact-2');
  await page.locator('input[type=file]').first().setInputFiles({
    name: 'large-fixture.bin', mimeType: 'application/octet-stream', buffer: Buffer.alloc(17 * 1024 * 1024, 7),
  });
  await page.getByRole('button', { name: '发送', exact: true }).click();
  await expect(page.locator('.message.self').filter({ hasText: 'large-fixture.bin' })).toBeVisible();
  await expect.poll(async () => {
    const state = await (await request.get('/api/state')).json();
    return state.messages.filter((m: any) => m.content === 'large-fixture.bin' && m.status === 'sent').length;
  }).toBe(1);
  expect(uploaded).toBe(1);
});
