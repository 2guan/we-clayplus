// 文件名：markdown-report.spec.ts
// 主要功能：验证洞察报告的 Markdown、数学公式、安全渲染和移动端布局。
// 作者：2guan
import {test,expect} from '@playwright/test';
test('报告支持 Markdown 与公式且不会执行 HTML',async({page})=>{
 const result='# 分析结论\n\n**重点**和行内公式 $E=mc^2$。\n\n$$\n\\frac{1}{n}\\sum_{i=1}^{n} x_i\n$$\n\n| 指标 | 数值 |\n| --- | --- |\n| 活跃 | 12 |\n\n- 待办事项\n\n```js\nconst x = 1;\n```\n\n<script>window.reportUnsafe=true</script>\n\n[危险链接](javascript:alert(1))';
 await page.route('**/api/state',async route=>{const r=await route.fetch();const s=await r.json();s.tasks=[{id:'fixture-report',kind:'insight',insight:{type:'conversation',prompt:'测试 Markdown 报告'},status:'completed',result,created:new Date().toISOString()}];await route.fulfill({json:s})});
 await page.goto('/#insights');const report=page.locator('.markdown-report');
 await expect(report.getByRole('heading',{name:'分析结论'})).toBeVisible();
 await expect(report.locator('strong')).toHaveText('重点');await expect(report.locator('.katex')).toHaveCount(2);
 await expect(report.locator('table')).toContainText('活跃');await expect(report.locator('pre')).toContainText('const x = 1');
 await expect(report.locator('script')).toHaveCount(0);expect(await page.evaluate(()=>(window as any).reportUnsafe)).toBeUndefined();
 await expect(report.locator('a')).not.toHaveAttribute('href',/javascript:/);
 await page.setViewportSize({width:390,height:844});
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
 const download=page.waitForEvent('download');await page.getByRole('button',{name:'导出报告'}).click();expect((await download).suggestedFilename()).toBe('分析报告.md');
});
