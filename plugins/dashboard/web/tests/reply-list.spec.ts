// 文件名：reply-list.spec.ts
// 主要功能：脱机验证默认黑名单及名单模式切换的配置展示。
// 作者：2guan
import {test,expect} from '@playwright/test';
test('回复策略默认空黑名单，按模式显示名单',async({page,request})=>{
 await request.post('/api/action',{data:{kind:'mock-reset'}});
 await page.goto('/#models?tab=global');
 await expect(page.getByRole('button',{name:'全局对话设置',exact:true})).toHaveAttribute('aria-pressed','true');
 const labels=await page.locator('.model-center .tabs button').allTextContents();
 expect(labels.indexOf('全局对话设置')).toBe(labels.indexOf('行为与任务')-1);
 await expect(page.locator('.pipeline-step')).toHaveCount(0);
 await expect(page.getByRole('heading',{name:'自动回复规则'})).toBeVisible();
 await expect(page.getByRole('button',{name:'对话模型',exact:true})).toHaveCount(1);
 await expect(page.getByText('人工发言自动接管')).toHaveCount(0);
 await page.goto('/#settings');
 await expect(page.getByRole('button',{name:'全局对话设置',exact:true})).toHaveCount(0);
 await page.goto('/#models?tab=global');
 const mode=page.getByRole('combobox',{name:'名单模式',exact:true});
 await expect(mode).toHaveValue('黑名单');
 await expect(page.getByRole('textbox',{name:'黑名单',exact:true})).toHaveValue('');
 await expect(page.getByRole('textbox',{name:'白名单',exact:true})).toHaveCount(0);
 await mode.selectOption('白名单');
 await expect(page.getByRole('textbox',{name:'白名单',exact:true})).toBeVisible();
 await expect(page.getByRole('textbox',{name:'黑名单',exact:true})).toHaveCount(0);
});
