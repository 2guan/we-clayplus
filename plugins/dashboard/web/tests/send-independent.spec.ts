// 文件名：send-independent.spec.ts
// 主要功能：验证未收到回执时可连续发送，旧请求完成或失败不清除新草稿。
// 作者：2guan
import {test,expect} from '@playwright/test';
test('两条消息独立发送，回执不锁住编辑器',async({page})=>{
 const pending: (()=>Promise<void>)[]=[];const bodies:any[]=[];
 await page.route('**/api/action',async route=>{
  const body=route.request().postDataJSON();if(body.kind!=='send')return route.continue();
  bodies.push(body);
  await new Promise<void>(resolve=>{pending.push(async()=>{await route.fulfill({status:body.content==='第一条'?502:200,json:body.content==='第一条'?{error:'未收到回执'}:{ok:true}});resolve()})});
 });
 await page.goto('/#wechat?target=mock-contact-2');
 const input=page.getByPlaceholder('输入消息…');const send=page.getByRole('button',{name:'发送',exact:true});
 await input.fill('第一条');await send.click();await expect(input).toHaveValue('');
 await input.fill('第二条');await expect(send).toBeEnabled();await send.click();
 await expect.poll(()=>bodies.length).toBe(2);expect(bodies.map(b=>b.content)).toEqual(['第一条','第二条']);
 await input.fill('第三条还在编写');
 for(const finish of pending)await finish();
 await expect(input).toHaveValue('第三条还在编写');await expect(send).toBeEnabled();
});
