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

test('聊天超过100条后，发送仍滚动到底部',async({page,request})=>{
 const state=await (await request.get('/api/state')).json();
 const target='mock-contact-2';
 state.messages=Array.from({length:120},(_,i)=>({id:`scroll-fixture-${i}`,target,sender:'测试好友',content:`历史消息 ${i}`,kind:'text',self:false,status:'sent',time:new Date().toISOString()}));
 await page.route('**/api/state',route=>route.fulfill({json:state}));
 await page.route('**/api/action',async route=>{
  const body=route.request().postDataJSON();
  if(body.kind!=='send')return route.continue();
  state.messages.push({id:`scroll-fixture-${state.messages.length}`,target,sender:'我',content:body.content,kind:'text',self:true,status:'sent',time:new Date().toISOString()});
  await route.fulfill({json:{ok:true}});
 });
 await page.goto(`/#wechat?target=${target}`);
 const list=page.locator('.message-list');
 await expect(list.locator('.message')).toHaveCount(100);
 for(const content of ['新的第一条','新的第二条']) {
  await list.evaluate(el=>{el.scrollTop=0});
  await expect.poll(()=>list.evaluate(el=>el.scrollHeight-el.clientHeight-el.scrollTop)).toBeGreaterThan(100);
  await page.getByPlaceholder('输入消息…').fill(content);
  await page.getByRole('button',{name:'发送',exact:true}).click();
  await expect(list.locator('.message.self').filter({hasText:content})).toBeInViewport();
  await expect.poll(()=>list.evaluate(el=>Math.abs(el.scrollHeight-el.clientHeight-el.scrollTop))).toBeLessThan(2);
  await expect(list.locator('.message')).toHaveCount(100);
 }
});
