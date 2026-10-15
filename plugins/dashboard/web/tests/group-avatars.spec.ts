// 文件名：group-avatars.spec.ts
// 主要功能：验证非好友群成员头像优先补查、消息自带头像及高优先级加载。
// 作者：2guan
import {test,expect} from '@playwright/test';
test('群成员头像不依赖好友通讯录',async({page})=>{
 await page.route('**/api/state',async route=>{const r=await route.fetch();const s=await r.json();s.contacts.push({id:'fixture@chatroom',name:'头像测试群',kind:'group',tags:[],mode:'manual'});s.messages=[{id:'avatar-old',target:'fixture@chatroom',sender_id:'fixture-member',sender:'非好友成员',content:'历史消息',kind:'text',self:false,status:'sent',time:new Date().toISOString()},{id:'avatar-new',target:'fixture@chatroom',sender_id:'fixture-new',sender:'新消息成员',sender_avatar:'https://wx.qlogo.cn/new-avatar',content:'新消息',kind:'text',self:false,status:'sent',time:new Date().toISOString()}];await route.fulfill({json:s})});
 let calls=0;
 await page.route('**/api/room?**',route=>{calls++;return route.fulfill({json:{members:[{id:'fixture-member',avatar:'https://wx.qlogo.cn/member-avatar'}]}})});
 await page.route('**/api/wechat-image?**',route=>route.fulfill({contentType:'image/svg+xml',body:'<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"/>'}));
 await page.goto('/#wechat?target=fixture%40chatroom');
 const old=page.getByRole('button',{name:'查看非好友成员的资料'}).locator('img');
 await expect(old).toHaveAttribute('src',/member-avatar/);await expect(old).toHaveAttribute('fetchpriority','high');
 await expect(page.getByRole('button',{name:'查看新消息成员的资料'}).locator('img')).toHaveAttribute('src',/new-avatar/);
 expect(calls).toBe(1);
});
