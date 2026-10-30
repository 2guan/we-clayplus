import { expect, test } from "@playwright/test";
for (const mobile of [false,true]) test(`朋友圈随机互动进度、停止及去重设置 ${mobile?"手机":"桌面"}`,async({page})=>{
 if(mobile)await page.setViewportSize({width:390,height:844});
 let job={id:"",kind:"like",status:"",total:0,checked:0,sent:0,skipped:0,message:""};
 const actions:unknown[]=[];
 await page.route("**/api/moments?*",r=>r.fulfill({json:{items:[],unread:0}}));
 await page.route("**/api/moments/random",r=>{
  if(r.request().method()==="POST"){
   const q=r.request().postDataJSON();actions.push(q);
   if(q.stop)job={...job,status:"cancelled",message:"任务已停止"};
   else job={...job,id:"fixture-job",kind:q.kind,status:"running",total:8,checked:2,sent:1,skipped:1,message:"正在处理测试好友的动态"};
  }
  return r.fulfill({json:job});
 });
 await page.goto("/#wechat");
 await page.getByRole("button",{name:mobile?"打开朋友圈":"朋友圈",exact:true}).click();
 const panel=page.getByRole("dialog",{name:"朋友圈",exact:true});
 await expect(panel.getByText("最近 24 小时 · 点赞 80% · 评论 50% · 评论间隔 15–30 秒")).toBeVisible();
 await panel.getByRole("button",{name:"随机点赞",exact:true}).click();
 await expect(panel.getByRole("button",{name:"随机点赞",exact:true})).toBeDisabled();
 await expect(panel.getByRole("button",{name:"随机评论",exact:true})).toBeDisabled();
 await expect(panel.getByRole("status").filter({hasText:"已检查"})).toContainText("成功 1，跳过 1");
 await panel.getByRole("button",{name:"停止",exact:true}).click();
 await expect(panel.getByRole("button",{name:"随机评论",exact:true})).toBeEnabled();
 await panel.getByRole("button",{name:"随机评论",exact:true}).click();
 await expect(panel.getByRole("button",{name:"停止",exact:true})).toBeVisible();
 expect(actions).toEqual([{kind:"like"},{stop:"fixture-job"},{kind:"comment"}]);
 await page.screenshot({path:`/tmp/weclay-moments-random-${mobile?"mobile":"desktop"}.png`});
 await panel.getByRole("button",{name:"关闭",exact:true}).click();
 if(mobile)await page.getByRole("button",{name:"打开导航",exact:true}).click();
 await page.getByRole("link",{name:"模型中心",exact:true}).click();
 await page.getByRole("button",{name:"全局对话设置",exact:true}).click();
 const settings=page.locator("form").filter({has:page.getByRole("heading",{name:"朋友圈随机互动",exact:true})});
 await expect(settings.getByRole("spinbutton",{name:"随机点赞概率 (%)",exact:true})).toHaveValue("80");
 await expect(settings.getByRole("spinbutton",{name:"随机评论概率 (%)",exact:true})).toHaveValue("50");
 await expect(settings.getByRole("switch",{name:"对方回复后按概率回应",exact:true})).toBeChecked();
 await settings.getByRole("spinbutton",{name:"评论最短间隔 (秒)",exact:true}).fill("31");
 await settings.getByRole("button",{name:"保存设置",exact:true}).click();
 await expect(settings.getByRole("alert")).toContainText("最短间隔不能大于最长间隔");
 await settings.getByRole("spinbutton",{name:"评论最短间隔 (秒)",exact:true}).fill("15");
 await settings.getByRole("spinbutton",{name:"点赞评论追溯时间 (小时)",exact:true}).fill("48");
 await settings.getByRole("button",{name:"保存设置",exact:true}).click();
 await expect(settings).toContainText("已与服务器同步");
 // 恢复脱机服务默认设置，避免影响共用 mock 的后续测试。
 await settings.getByRole("spinbutton",{name:"点赞评论追溯时间 (小时)",exact:true}).fill("24");
 await settings.getByRole("button",{name:"保存设置",exact:true}).click();
 await expect(settings).toContainText("已与服务器同步");
});
