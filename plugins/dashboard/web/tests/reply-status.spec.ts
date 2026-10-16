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

test('模型失败后可手动重试，处理中防止重复提交',async({page})=>{
 let mode='auto';
 let task={id:'fixture-failed-reply',source:'wechat',target:'mock-contact-2',kind:'chat',status:'failed',reply_retryable:true,auto_retries:1,error:'模型响应读取超时'};
 let clicks=0;let reject=true;let release:()=>void=()=>{};
 const gate=new Promise<void>(resolve=>{release=resolve});
 await page.route('**/api/state',async route=>{const response=await route.fetch();const state=await response.json();state.settings['reply.enabled']=true;state.account.status='online';state.contacts.find((c:any)=>c.id===task.target).mode=mode;state.tasks=[{...task}];await route.fulfill({json:state})});
 await page.route('**/api/action',async route=>{
  const body=route.request().postDataJSON();if(body.kind!=='retry-reply')return route.continue();
  expect(body).toMatchObject({id:'fixture-failed-reply',target:'mock-contact-2'});clicks++;
  if(clicks===1)await gate;
  if(reject)return route.fulfill({status:400,json:{error:'原消息已撤回，无法重试'}});
  task={...task,id:'fixture-child-reply',status:'running',reply_retryable:false,auto_retries:0};
  await route.fulfill({json:{ok:true}});
 });
 await page.goto('/#wechat?target=mock-contact-2');
 const failure=page.locator('.chat-reply-error');const retry=failure.getByRole('button',{name:'重试',exact:true});
 await expect(failure).toContainText('自动重试后仍未完成回复');await expect(failure).toContainText('模型响应读取超时');
 await retry.click();await expect(retry).toBeDisabled();await expect(retry).toHaveAttribute('aria-busy','true');expect(clicks).toBe(1);
 release();await expect(failure).toContainText('原消息已撤回，无法重试');await expect(retry).toBeEnabled();
 reject=false;await retry.click();await expect(failure).toHaveCount(0);await expect(page.locator('.chat-reply-status')).toHaveText('正在回复…');expect(clicks).toBe(2);
 task={...task,status:'running',auto_retries:1};await page.reload();await expect(page.locator('.chat-reply-status')).toHaveText('正在重试（1/1）…');
 task={...task,status:'failed',reply_retryable:true};mode='manual';await page.reload();await expect(retry).toBeDisabled();await expect(failure).toContainText('恢复自动回复后可重试');
 mode='auto';await page.setViewportSize({width:390,height:844});await page.reload();await expect(retry).toBeVisible();await expect(retry).toBeEnabled();
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
 await page.screenshot({path:'test-results/reply-retry-mobile.png',fullPage:true});
 await page.goto('/#wechat?target=mock-contact-1');await expect(failure).toHaveCount(0);
});
