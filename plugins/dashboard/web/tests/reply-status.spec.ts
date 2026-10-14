// 文件名：reply-status.spec.ts
// 主要功能：验证回复提示只跟随当前会话真实自动回复任务。
// 作者：2guan
import {test,expect} from '@playwright/test';
test('回复提示按来源、目标和任务生命周期显示',async({page})=>{
 let task={source:'wechat',target:'mock-contact-2',kind:'chat',status:'running'};
 let mode='auto';
 await page.route('**/api/state',async route=>{const r=await route.fetch();const s=await r.json();s.settings['reply.enabled']=true;s.contacts.find((c:any)=>c.id==='mock-contact-2').mode=mode;s.tasks=[{...task,id:'fixture-reply'}];await route.fulfill({json:s})});
 await page.goto('/#wechat?target=mock-contact-2');await expect(page.locator('.chat-reply-status')).toHaveText('正在回复…');
 for(const status of ['completed','failed','cancelled']) {task.status=status;await page.reload();await expect(page.locator('.chat-reply-status')).toHaveCount(0)}
 task.status='queued';await page.reload();await expect(page.locator('.chat-reply-status')).toBeVisible();
 task.source='playground';await page.reload();await expect(page.locator('.chat-reply-status')).toHaveCount(0);
 task.source='wechat';task.target='another';await page.reload();await expect(page.locator('.chat-reply-status')).toHaveCount(0);
 task.target='mock-contact-2';mode='manual';await page.reload();await expect(page.locator('.chat-reply-status')).toHaveCount(0);
});
