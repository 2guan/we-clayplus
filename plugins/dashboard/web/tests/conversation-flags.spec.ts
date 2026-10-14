// 文件名：conversation-flags.spec.ts
// 主要功能：脱机验证置顶排序及免打扰群红点，不执行真实微信设置操作。
// 作者：2guan
import {test,expect} from '@playwright/test';
test('微信置顶排序与群免打扰红点',async({page})=>{
 await page.route('**/api/state',async route=>{const r=await route.fetch();const s=await r.json();const base=s.contacts[0];s.contacts=[
 {...base,id:'fixture-normal',name:'普通会话',remark:'',kind:'friend',pinned:false,unread:8,time:'2026-10-14T10:00:00Z'},
 {...base,id:'fixture-muted@chatroom',name:'免打扰测试群',remark:'',kind:'group',pinned:true,wechat_pinned:true,muted:true,unread:123,time:'2026-10-12T10:00:00Z'},
 {...base,id:'fixture-pinned',name:'置顶好友',remark:'',kind:'friend',pinned:true,wechat_pinned:true,unread:3,time:'2026-10-13T10:00:00Z'}];await route.fulfill({json:s})});
 await page.goto('/#wechat');
 const rows=page.locator('.conversation');await expect(rows.nth(0)).toContainText('置顶好友');await expect(rows.nth(1)).toContainText('免打扰测试群');
 await expect(rows.nth(1).locator('.unread-dot')).toBeVisible();await expect(rows.nth(1).locator('.unread')).toHaveCount(0);
 await expect(rows.nth(2).locator('.unread')).toHaveText('8');
});

// 只读取脱机群标题，模式切换的请求拦截验证，不触发真实账号操作。
test('群人数与精简回复模式标签',async({page})=>{
 await page.route('**/api/state',async route=>{const r=await route.fetch();const s=await r.json();s.contacts=[{...s.contacts[0],id:'fixture-room@chatroom',name:'人数测试群',remark:'',kind:'group',members:483,mode:'manual'}];await route.fulfill({json:s})});
 await page.goto('/#wechat?target=fixture-room%40chatroom');
 const header=page.locator('.chat-title');
 await expect(header.locator('h2')).toHaveText('人数测试群(483)');
 await expect(header.locator('small')).toHaveCount(0);
 await expect(header.getByRole('button',{name:'手动',exact:true})).toHaveAttribute('aria-pressed','true');
 await expect(header.getByRole('button',{name:'自动',exact:true})).toHaveAttribute('aria-pressed','false');
});

// 默认入口直接展开全局设置，通讯录明确区分继承和单独设置。
test('通讯录默认模式与模型中心入口',async({page})=>{
 await page.route('**/api/state',async route=>{const r=await route.fetch();const s=await r.json();s.contacts=[{...s.contacts[0],id:'fixture-default',name:'默认测试好友',remark:'',kind:'friend',mode:'manual',mode_inherited:true}];await route.fulfill({json:s})});
 await page.goto('/#contacts');
 await expect(page.getByRole('combobox',{name:'默认测试好友回复模式'})).toHaveValue('inherit');
 await page.getByRole('link',{name:'默认回复模式'}).click();
 await expect(page.getByText('好友默认模式',{exact:true})).toBeVisible();
 await expect(page.getByText('群聊默认模式',{exact:true})).toBeVisible();
});
