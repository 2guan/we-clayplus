// 文件名：refresh-load.spec.ts
// 主要功能：验证密集实时事件不会并发下载多份完整状态。
// 作者：2guan
import {test,expect} from '@playwright/test';
test('突发更新合并为串行状态读取',async({page})=>{
 await page.addInitScript(()=>{(window as any).EventSource=class extends EventTarget{constructor(){super();(window as any).fixtureEvents=this}close(){}}});
 let active=0,maxActive=0,calls=0;
 await page.route('**/api/state',async route=>{active++;calls++;maxActive=Math.max(maxActive,active);try{await new Promise(r=>setTimeout(r,150));const response=await route.fetch();await route.fulfill({response})}finally{active--}});
 await page.goto('/#overview');
 await page.waitForFunction(()=>(window as any).fixtureEvents);
 await page.evaluate(()=>{for(let i=0;i<30;i++)(window as any).fixtureEvents.dispatchEvent(new Event('update'))});
 await expect(page.locator('.page-title')).toBeVisible();
 await expect.poll(()=>active).toBe(0);
 expect(maxActive).toBe(1);expect(calls).toBeLessThanOrEqual(3);
});
