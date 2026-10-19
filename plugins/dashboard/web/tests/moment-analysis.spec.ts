// 文件名：moment-analysis.spec.ts
// 主要功能：验证后端资料缓存、详情追问、合并导出及空报告重试入口。
// 作者：2guan
import { readFile } from "node:fs/promises";
import { test, expect } from "@playwright/test";
test.beforeEach(async({request})=>{await request.post("/api/action",{data:{kind:"mock-reset"}})});
test("资料准备在后台进行，重复分析使用缓存，追问保留原文并合并导出",async({page})=>{
 await page.goto("/#insights?tab=moments");
 const target=page.getByRole("combobox",{name:"分析对象"});await target.fill("mock-contact-1");await page.getByRole("listbox").getByRole("option").click();
 let browserReads=0;page.on("request",r=>{if(r.url().includes("/api/moments?"))browserReads++});
 const create=page.waitForRequest(r=>r.url().endsWith("/api/reports")&&r.method()==="POST");
 await page.getByRole("button",{name:"生成分析报告",exact:true}).click();
 expect((await create).postDataJSON()).toMatchObject({type:"moments",target:"mock-contact-1",range:{mode:"count",count:50},refresh:false});
 await expect(page.locator(".report-detail")).toContainText("已完成");
 await expect(page.locator(".report-source-note")).toContainText("北京时间");
 await page.getByLabel("追问内容").fill("请展开第三点，并列出依据");await page.getByRole("button",{name:"生成补充",exact:true}).click();
 await expect(page.locator(".report-supplement")).toContainText("请展开第三点，并列出依据");
 await expect(page.locator(".report-supplement .task-result")).toBeVisible();
 const [file]=await Promise.all([page.waitForEvent("download"),page.getByRole("button",{name:"导出报告与追问",exact:true}).click()]);
 expect(file.suggestedFilename()).toBe("分析报告与追问.md");
 const exported=await readFile((await file.path())!,"utf8");expect(exported).toContain("## 补充 1");expect(exported).toContain("请展开第三点，并列出依据");expect(exported).toContain("会话摘要");
 await page.reload();await expect(page.locator(".report-supplement")).toContainText("请展开第三点");
 await page.getByRole("button",{name:"生成分析报告",exact:true}).click();await expect(page.locator(".report-source-note")).toContainText("使用缓存资料");
 expect(browserReads).toBe(0);
 await page.setViewportSize({width:390,height:844});await page.reload();
 await expect(page.getByLabel("追问内容")).toBeVisible();
 expect(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth)).toBe(false);
 await page.getByLabel("追问内容").scrollIntoViewIfNeeded();
 await page.screenshot({path:"test-results/report-followup-mobile.png",fullPage:true});
});

test("详情中空报告可以重试，旧报告明确要求补选范围",async({page,request})=>{
 const original=await (await request.get("/api/state")).json();
 const template={id:"fixture-empty",kind:"insight",name:"会话总结",target:"mock-contact-0",created:new Date().toISOString(),status:"failed",input:"请总结",model:"mock",result:"",error:"fixture timeout",insight:{managed:true,root_id:"fixture-empty",type:"conversation",prompt:"请总结",snapshot_id:"fixture-source"}};
 await page.route("**/api/state",async route=>route.fulfill({json:{...original,tasks:[template]}}));
 let attempts=0;
 await page.route("**/api/reports/action",async route=>{attempts++;expect(route.request().postDataJSON()).toMatchObject({id:"fixture-empty",operation:"retry"});await route.fulfill({json:{id:"fixture-retry"}})});
 await page.goto("/#insights?tab=history");await page.getByRole("button",{name:"查看任务",exact:true}).click();
 const modal=page.getByRole("dialog");await expect(modal.getByText("这次任务没有完成")).toBeVisible();
 await expect(modal.getByRole("button",{name:"生成补充",exact:true})).toBeDisabled();
 await modal.getByRole("button",{name:"重试",exact:true}).click();expect(attempts).toBe(1);
 await page.unroute("**/api/state");
 await page.route("**/api/state",async route=>route.fulfill({json:{...original,tasks:[{...template,insight:{type:"conversation",prompt:"请总结"}}]}}));
 await page.reload();await page.getByRole("button",{name:"查看任务",exact:true}).click();
 await page.getByRole("dialog").getByRole("button",{name:"重试",exact:true}).click();
 await expect(page.getByRole("dialog").locator(".report-rebuild")).toContainText("旧报告没有完整资料快照");
 await expect(page.getByRole("dialog").getByLabel("重新读取条数")).toBeVisible();
});
