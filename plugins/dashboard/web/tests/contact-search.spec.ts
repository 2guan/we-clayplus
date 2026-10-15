import { test, expect } from '@playwright/test';

test('通讯录和会话支持全拼、首字母，列表及洞察选择保持稳定', async ({page}) => {
 let reverse = false;
 await page.route('**/api/state',async route => {
  const response=await route.fetch(); const state=await response.json();
  const base=state.contacts.find((c:any)=>c.kind==='friend');
  const contacts=[
   {...base,id:'fixture-zhang',name:'张三',remark:'王经理',tags:['测试标签'],preview:'最近消息',time:'2026-01-01T10:00:00Z'},
   {...base,id:'fixture-li',name:'李四',remark:'',tags:[],preview:'最近消息',time:'2026-01-01T10:00:00Z'},
  ];
  state.contacts=reverse ? contacts.reverse() : contacts;
  await route.fulfill({json:state});
 });
 await page.goto('/#contacts');
 const rows=page.locator('tbody tr');
 await expect(rows.first()).toContainText('李四');
 const search=page.getByRole('textbox',{name:'搜索联系人'});
 for(const query of ['zhangsan','zs','WANGJINGLI','wj l','fixture-zhang','测试标签']) {
  await search.fill(query);
  await expect(rows).toHaveCount(1); await expect(rows).toContainText('王经理');
 }
 await search.fill('不存在'); await expect(rows).toHaveCount(0);
 reverse=true;
 await page.reload(); await expect(rows.first()).toContainText('李四');
 await page.goto('/#wechat');
 const chatSearch=page.getByRole('textbox',{name:'搜索会话'});
 for(const query of ['zhangsan','zs','wangjingli','wjl']) {
  await chatSearch.fill(query);
  await expect(page.locator('.conversation')).toHaveCount(1);
  await expect(page.locator('.conversation')).toContainText('王经理');
 }
 await page.goto('/#insights');
 const target=page.getByRole('combobox',{name:'分析对象'});
 await target.fill('zs'); await page.getByRole('option',{name:/王经理/}).click();
 await expect(target).toHaveValue('王经理');
 await page.getByRole('link',{name:'通讯录',exact:true}).click();
 reverse=false;
 await page.getByRole('link',{name:'智能洞察',exact:true}).click();
 await expect(target).toHaveValue('王经理');
 await page.reload(); await expect(target).toHaveValue('王经理');
});

test('通讯录中英文按拼音字母混排，特殊名称在末尾，标签顺序固定', async ({page}) => {
 await page.route('**/api/state',async route=>{
  const response=await route.fetch(); const state=await response.json();
  const base=state.contacts.find((c:any)=>c.kind==='friend');
  state.contacts=['🙂小王','张三','!小李','Bob','李四','阿明','Alice','123'].map((name,i)=>({...base,id:`order-fixture-${i}`,name,remark:''}));
  await route.fulfill({json:state});
 });
 await page.goto('/#contacts');
 await expect(page.locator('tbody .contact-cell strong')).toHaveText(['Alice','阿明','Bob','李四','张三','!小李','🙂小王','123']);
 const tabs=page.locator('main').getByRole('button').filter({hasText:/^(微信好友|微信群聊|公众号 \/ 订阅号|系统账号|企业微信) \(/});
 await expect(tabs).toHaveText(['微信好友 (8)','微信群聊 (0)','公众号 / 订阅号 (0)','系统账号 (0)','企业微信 (0)']);
});
