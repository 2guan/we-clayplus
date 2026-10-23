// 文件名：target-probability.spec.ts
// 主要功能：脱机验证会话设置的全局概率展示、单独保存及恢复继承。
// 作者：2guan
import {test,expect} from '@playwright/test';
for(const width of [1440,390]) test(`群专属概率 ${width}`,async({page,request})=>{
 await page.setViewportSize({width,height:900});
 await request.post('/api/action',{data:{kind:'mock-reset'}});
 const state=await (await request.get('/api/state')).json();
 const group=state.contacts.find((c:{kind:string})=>c.kind==='group');
 await page.goto(`/#wechat?target=${group.id}`);
 await page.getByRole('button',{name:'会话资料与专属策略',exact:true}).click();
 const dialog=page.getByRole('dialog');
 await expect(dialog.getByText('地区未提供')).toHaveCount(0);
 await expect(dialog.getByText('群聊停顿 7 秒后合并', {exact:false})).toBeVisible();
 await expect(dialog.getByText('模型可选择不回复', {exact:false})).toBeVisible();
 const mode=dialog.getByRole('combobox',{name:'群回复概率来源'});
 const input=dialog.getByRole('spinbutton',{name:'群回复概率',exact:true});
 await expect(mode).toHaveValue('default');
 await expect(input).toHaveValue(String(state.settings['reply.probability']));
 await expect(input).toBeDisabled();
 await mode.selectOption('custom');await input.fill('0');
 await dialog.getByRole('button',{name:'保存专属策略'}).click();
 await expect.poll(async()=> (await (await request.get('/api/state')).json()).overrides[group.id]?.probability).toBe(0);
 await mode.selectOption('default');await dialog.getByRole('button',{name:'保存专属策略'}).click();
 await expect.poll(async()=> (await (await request.get('/api/state')).json()).overrides[group.id]?.probability).toBeUndefined();
 expect(await dialog.evaluate(el=>el.scrollWidth<=el.clientWidth)).toBe(true);
 const sourceBox=await mode.boundingBox();const valueBox=await input.boundingBox();
 expect(Math.abs(sourceBox!.width-valueBox!.width)).toBeLessThan(2);
 await dialog.getByRole('button',{name:'设置备注',exact:true}).click();
 const remarkDialog=page.getByRole('dialog',{name:'设置备注',exact:true});
 const remarkInput=remarkDialog.getByRole('textbox',{name:'备注名称'});
 await expect(remarkInput).toBeFocused();
 const fullWidth=await remarkDialog.locator('form').boundingBox();
 expect(Math.abs((await remarkInput.boundingBox())!.width-fullWidth!.width)).toBeLessThan(2);
 await remarkInput.fill('脱机备注测试');
 await remarkDialog.getByRole('button',{name:'保存备注'}).click();
 await expect(remarkDialog).toHaveCount(0);
 await expect.poll(async()=> (await (await request.get('/api/state')).json()).contacts.find((c:{id:string})=>c.id===group.id).remark).toBe('脱机备注测试');

});
