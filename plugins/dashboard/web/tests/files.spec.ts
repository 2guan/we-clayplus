// 验证入站文件无需已有媒体 URL 即显示下载入口，上传通知不提供虚假下载。
import {test,expect} from '@playwright/test';
test('文件完成后原通知显示为一张可下载卡片',async({page})=>{
 await page.route('**/api/state',async route=>{
  const response=await route.fetch();const state=await response.json();
  state.contacts.push({id:'fixture-file-peer',name:'文件测试',kind:'friend',tags:[],mode:'manual'});
  state.messages=[{id:'fixture-pending',target:'fixture-file-peer',sender:'测试好友',content:'测试文档.pdf',kind:'file',self:false,status:'sent',time:new Date().toISOString(),rich:{status:'uploading',description:'文件上传中…'}},{rich:{file_notice_id:'fixture-pending'},id:'fixture-file',target:'fixture-file-peer',sender:'测试好友',content:'测试文档.pdf',kind:'file',self:false,status:'sent',time:new Date().toISOString()}];
  await route.fulfill({json:state});
 });
 await page.route('**/api/message-media?**',route=>route.fulfill({contentType:'application/octet-stream',headers:{'Content-Disposition':'attachment; filename="fixture.pdf"'},body:'%PDF-1.4 fixture'}));
 await page.goto('/#wechat?target=fixture-file-peer');
 await expect(page.locator('.file-message')).toHaveCount(1);
 await expect(page.locator('.file-message > svg')).toHaveCount(1);
 const download=page.getByRole('link',{name:'下载文件',exact:true});
 await expect(download).toHaveCount(1);await expect(download).toHaveAttribute('href',/target=fixture-file-peer&id=fixture-file/);
 const done=page.waitForEvent('download');await download.click();expect((await done).suggestedFilename()).toBe('测试文档.pdf');
 await page.route('**/api/message-media?**',route=>route.fulfill({status:502,json:{error:'下载失败'}}));
 await download.click();
 await expect(page.getByText('文件下载失败，请稍后重试或在微信中打开',{exact:true})).toBeVisible();
 await expect(page).toHaveURL(/#wechat\?target=fixture-file-peer/);
 await expect(download).toHaveAttribute('aria-disabled','false');
});

import { mergeFileMessages } from '../src/fileMessages';
import type { Message } from '../src/types';
test('文件更新保留原位置与时间，不合并其他发送者或会话的同名文件',()=>{
 const pending:Message={id:'9007199254740992',target:'fixture-room',sender:'测试好友',sender_id:'fixture-member',content:'同名.pdf',kind:'file',self:false,status:'sent',source:'wechat',time:'2026-01-01T10:00:00Z',rich:{status:'uploading'}};
 const complete:Message={...pending,id:'9007199254740993',time:'2026-01-01T10:01:00Z',rich:{file_notice_id:pending.id}};
 const middle:Message={...pending,id:'middle',kind:'text',rich:undefined};
 expect(mergeFileMessages([pending])[0].rich?.status).toBe('uploading');
 for(const ordered of [[pending,middle,complete],[complete,pending,middle]]) {
  const result=mergeFileMessages(ordered);
  expect(result.map(m=>m.id)).toEqual([complete.id,middle.id]);
  expect(result[0]).toMatchObject({displayID:pending.id,time:pending.time});
  expect(result[0].rich?.status).toBeUndefined();
 }
 for(const unrelated of [{...complete,target:'other-room'},{...complete,sender_id:'other-member'},{...complete,self:true},{...complete,rich:undefined}]) {
  expect(mergeFileMessages([pending,unrelated])).toHaveLength(2);
 }
 expect(pending.rich?.status).toBe('uploading');
});
