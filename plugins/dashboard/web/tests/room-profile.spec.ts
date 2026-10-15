// 文件名：room-profile.spec.ts
// 主要功能：脱机验证标题资料入口、群成员搜索和备注编辑，不操作真实账号。
// 作者：2guan
import {test,expect} from '@playwright/test';
test('标题打开群资料和人员资料',async({page,request})=>{
 await request.post('/api/action',{data:{kind:'mock-reset'}});
 const s=await (await request.get('/api/state')).json();const group=s.contacts.find((c:any)=>c.kind==='group');const friend=s.contacts.find((c:any)=>c.kind==='friend');
 await page.goto(`/#wechat?target=${group.id}`);await page.locator('.chat-title-profile').click();
 const room=page.getByRole('dialog',{name:'群资料',exact:true});await expect(room.getByText('这是一条脱机演示群公告。')).toBeVisible();
 await room.getByRole('textbox',{name:'搜索群成员'}).fill(friend.name);await expect(room.locator('.room-members > div')).toHaveCount(1);
 await room.getByRole('button',{name:'设置备注'}).click();await room.getByRole('textbox',{name:'群备注'}).fill('群备注测试');await room.getByRole('button',{name:'保存备注'}).click();
 await expect(room.getByText('群备注测试').first()).toBeVisible();await room.getByRole('button',{name:'关闭',exact:true}).click();
 await page.goto(`/#wechat?target=${friend.id}`);await page.reload();await page.locator('.chat-title-profile').click();
 const person=page.getByRole('dialog',{name:'人员资料'});await expect(person).toBeVisible();await person.getByRole('button',{name:'设置备注'}).click();
 await person.getByRole('textbox',{name:'备注名称'}).fill('好友备注测试');await person.getByRole('button',{name:'保存备注'}).click();await expect(person.getByRole('heading',{name:'好友备注测试'})).toBeVisible();
});
