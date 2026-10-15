// 文件名：resend.spec.ts
// 主要功能：脱机验证失败消息重发菜单和未知回执的重复发送确认。
// 作者：2guan
import {test,expect} from '@playwright/test';
test('失败与未知消息显示重发，未知回执须确认',async({page})=>{
 const calls:any[]=[];
 await page.route('**/api/state',async route=>{const r=await route.fetch();const s=await r.json();s.messages=['failed','unknown','sent'].map((status,i)=>({id:`retry-${i}`,target:'mock-contact-2',self:true,source:'manual',sender:'我',kind:'text',content:'测试',status,time:'2026-10-14T10:00:00Z'}));await route.fulfill({json:s})});
 await page.route('**/api/action',async route=>{calls.push(route.request().postDataJSON());await route.fulfill({json:{ok:true}})});
 await page.goto('/#wechat?target=mock-contact-2');
 await page.locator('.message').nth(0).locator('.bubble').click({button:'right'});await page.getByRole('menuitem',{name:'重发',exact:true}).click();
 await expect.poll(()=>calls.filter(c=>c.kind==='resend').length).toBe(1);
 await page.locator('.message').nth(1).locator('.bubble').click({button:'right'});page.once('dialog',d=>d.dismiss());await page.getByRole('menuitem',{name:'重发',exact:true}).click();
 expect(calls.filter(c=>c.kind==='resend')).toHaveLength(1);
 await page.locator('.message').nth(2).locator('.bubble').click({button:'right'});await expect(page.getByRole('menuitem',{name:'重发',exact:true})).toHaveCount(0);
});
