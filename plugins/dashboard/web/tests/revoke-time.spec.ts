// 文件名：revoke-time.spec.ts
// 主要功能：验证菜单打开时跨越两分钟后自动隐藏撤回入口。
// 作者：2guan
import {test,expect} from '@playwright/test';
test('过期撤回入口隐藏',async({page})=>{
 const now=new Date('2026-10-14T12:00:00Z');await page.clock.install({time:now});
 await page.route('**/api/state',async route=>{const r=await route.fetch();const s=await r.json();s.messages=[{id:'fixture',target:'mock-contact-2',self:true,source:'manual',status:'sent',kind:'text',content:'测试',time:new Date(now.getTime()-119000).toISOString()}];await route.fulfill({json:s})});
 await page.goto('/#wechat?target=mock-contact-2');await page.locator('.bubble').click({button:'right'});
 await expect(page.getByRole('menuitem',{name:'撤回',exact:true})).toBeVisible();
 await page.clock.fastForward(2000);await expect(page.getByRole('menuitem',{name:'撤回',exact:true})).toHaveCount(0);
});
