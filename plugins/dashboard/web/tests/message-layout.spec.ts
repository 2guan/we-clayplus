// 文件名：message-layout.spec.ts
// 主要功能：脱机验证来源折角、短消息宽度和双向侧边菜单布局。
// 作者：2guan
import {test, expect} from '@playwright/test';
for (const width of [1440,390]) test(`气泡侧边菜单与来源折角 ${width}`, async ({page})=>{
 await page.setViewportSize({width,height:900});
 await page.route('**/api/state',async route=>{
  const r=await route.fetch(); const s=await r.json();
  s.messages=[false,true,true].map((self,i)=>({id:`layout-${i}`,target:'mock-contact-2',self,sender:'测试',kind:'text',content:'好',source:i===2?'ai':'manual',status:'sent',time:'2026-10-14T10:00:00Z'}));
  await route.fulfill({json:s});
 });
 await page.goto('/#wechat?target=mock-contact-2');
 const rows=page.locator('.message'); await expect(rows).toHaveCount(3);
 await expect(page.locator('.message-meta')).toHaveCount(0);
 await expect(page.getByRole('img',{name:'人工发送',exact:true})).toHaveCount(1);
 await expect(page.getByRole('img',{name:'自动发送',exact:true})).toHaveCount(1);
 for (let i=0;i<3;i++) {
  const bubble=await rows.nth(i).locator('.bubble').boundingBox();
  const menu=await rows.nth(i).getByRole('button',{name:'消息操作'}).boundingBox();
  expect(bubble!.width).toBeLessThan(65);
  if(i===0) expect(menu!.x).toBeGreaterThan(bubble!.x+bubble!.width);
  else expect(menu!.x+menu!.width).toBeLessThan(bubble!.x);
 }
 await rows.nth(1).getByRole('button',{name:'消息操作'}).click();
 await expect(page.getByRole('menu')).toBeVisible();
});
