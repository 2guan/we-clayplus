import {test,expect} from "@playwright/test";

test("日志原文按需读取，正文深处搜索与导出保留完整内容",async({page})=>{
 const full={id:"fixture-raw-log",time:new Date().toISOString(),level:"DEBUG",component:"debug",message:"模型响应原文\n"+"fixture content ".repeat(300)+"fixture-deep-needle"};
 let reads=0;
 await page.route("**/api/state",async route=>{const r=await route.fetch();const st=await r.json();st.messages=[];st.logs=[{...full,message:full.message.slice(0,80)}];await route.fulfill({json:st});});
 await page.route("**/api/logs?**",route=>{const p=new URL(route.request().url()).searchParams;
  if(p.has("id")){reads++;return route.fulfill({json:full});}
  return route.fulfill({json:[{...full,message:p.get("full")==="1"?full.message:full.message.slice(0,80)}]});
 });
 await page.goto("/#logs");
 await expect(page.locator(".log-line")).toHaveCount(1);
 expect(reads).toBe(0);
 await page.getByLabel("搜索日志",{exact:true}).fill("fixture-deep-needle");
 await expect(page.locator(".log-line")).toHaveCount(1);
 await page.locator(".log-line").click();
 await expect(page.locator(".log-detail-message")).toHaveText(full.message);
 expect(reads).toBe(1);
 await page.getByRole("button",{name:"关闭",exact:true}).click();
 const download=page.waitForEvent("download");await page.getByRole("button",{name:"导出筛选结果"}).click();
 expect((await download).suggestedFilename()).toBe("we-clay-logs.log");
});

test("缓存清理按类确认，手机布局与头像刷新",async({page})=>{
 let posts=0;let version="old";
 const items=[{kind:"logs",name:"控制台日志",description:"清理日志，保留聊天归档。",count:10,bytes:50000},{kind:"avatars",name:"头像缓存",description:"浏览器管理，重新获取。",count:0,bytes:0},{kind:"media",name:"文件与媒体缓存",description:"保护音色样本。",count:2,bytes:10000}];
 await page.route("**/api/state",async route=>{const r=await route.fetch();const st=await r.json();st.settings["cache.avatar_version"]=version;await route.fulfill({json:st});});
 await page.route("**/api/cache**",route=>{
  if(route.request().method()==="POST"){const q=route.request().postDataJSON();expect(q.confirm).toBe(true);posts++;if(q.kind==="avatars")version="new";return route.fulfill({json:{count:10,bytes:50000}});}
  return route.fulfill({json:items});
 });
 await page.setViewportSize({width:390,height:844});
 await page.goto("/#settings?tab=cache");
 await expect(page.getByRole("heading",{name:"缓存清理",exact:true})).toBeVisible();
 await expect(page.getByRole("article",{name:"控制台日志"})).toContainText("10 项");
 page.once("dialog",d=>d.dismiss());await page.getByRole("button",{name:"清理控制台日志"}).click();expect(posts).toBe(0);
 page.once("dialog",d=>d.accept());await page.getByRole("button",{name:"清理控制台日志"}).click();await expect(page.getByRole("status")).toContainText("已清理 10 项");
 page.once("dialog",d=>d.accept());await page.getByRole("button",{name:"刷新头像缓存"}).click();await expect(page.getByRole("status")).toContainText("已刷新头像缓存");expect(version).toBe("new");
 expect(await page.evaluate(()=>document.documentElement.scrollWidth-innerWidth)).toBeLessThanOrEqual(0);
 await page.reload();await expect(page.getByRole("heading",{name:"缓存清理",exact:true})).toBeVisible();
});

test("真实缓存统计显示五类项目，桌面与手机布局完整",async({page})=>{
 for(const width of [1440,390]){
  await page.setViewportSize({width,height:900});await page.goto("/#settings?tab=cache");
  await expect(page.locator(".cache-item")).toHaveCount(5);
  if(width<768)await expect(page.locator(".sidebar")).not.toBeInViewport();
  await expect(page.getByRole("article",{name:"文件与媒体缓存"})).toContainText("音色样本");
  expect(await page.evaluate(()=>document.documentElement.scrollWidth-innerWidth)).toBeLessThanOrEqual(0);
  await page.screenshot({path:`/tmp/weclay-cache-${width}.png`,fullPage:true,animations:"disabled"});
 }
});
