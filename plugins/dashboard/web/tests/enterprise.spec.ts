// 文件名：enterprise.spec.ts
// 主要功能：脱机验证企业联系人标签、普通聊天入口和通讯录分类。
// 作者：2guan
import {test,expect} from '@playwright/test';
test('企业微信保留聊天入口并独立展示类型',async({page})=>{
 await page.route('**/api/state',async route=>{const r=await route.fetch();const s=await r.json();s.contacts=[{...s.contacts[0],id:'fixture@openim',name:'企业微信联系人 · fixture',remark:'',kind:'enterprise',enterprise:true,profile_note:'昵称资料暂未获取',mode:'manual',time:'2026-10-14T10:00:00Z'}];await route.fulfill({json:s})});
 await page.route('**/api/person?*',route=>route.fulfill({json:{id:'fixture@openim',name:'企业微信联系人 · fixture',kind:'enterprise',enterprise:true,profile_note:'当前 core 未返回企业联系人资料'}}));
 await page.goto('/#wechat?target=fixture%40openim');
 await expect(page.locator('.conversation .contact-kind')).toHaveText('企业微信');
 await page.locator('.chat-title-profile').click();const person=page.getByRole('dialog',{name:'人员资料'});await expect(person.getByText('当前 core 未返回企业联系人资料')).toBeVisible();await expect(person.getByRole('button',{name:'朋友圈'})).toHaveCount(0);
 await person.getByRole('button',{name:'关闭人员资料'}).click();await page.goto('/#contacts');await page.getByRole('button',{name:'企业微信 (1)',exact:true}).click();await expect(page.locator('.contact-cell')).toContainText('企业微信联系人');
});
