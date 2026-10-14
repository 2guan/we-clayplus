// 文件名：RoomProfile.tsx
// 主要功能：展示群资料、搜索群成员并编辑仅自己可见的群备注。
// 作者：2guan
import {useEffect,useState} from 'react';
import type {Contact,PageProps} from './types';
import {Avatar,Modal} from './ui';
import {request} from './api';
type Member={id:string;name:string;avatar:string;display_name:string};
type Room={name:string;announcement:string;notice?:string;self_name:string;members:Member[]};
export function RoomProfile(props:PageProps & {contact:Contact;onClose:()=>void}) {
 const {contact,act,notify}=props;
 const [room,setRoom]=useState<Room>();const [error,setError]=useState('');const [query,setQuery]=useState('');
 const [retry,setRetry]=useState(0);const [editing,setEditing]=useState(false);const [remark,setRemark]=useState(contact.remark);const [busy,setBusy]=useState(false);
 useEffect(()=>{let active=true;setError('');request<Room>(`/api/room?id=${encodeURIComponent(contact.id)}`).then(r=>{if(active)setRoom(r)}).catch(e=>{if(active)setError(e.message)});return()=>{active=false}},[contact.id,retry]);
 const members=room?.members.filter(m=>`${m.name} ${m.display_name} ${m.id}`.toLowerCase().includes(query.toLowerCase()))||[];
 return <Modal open onClose={props.onClose} title="群资料" description={contact.remark||contact.name}>
  <div className="room-profile">
   <input aria-label="搜索群成员" placeholder="搜索群成员…" value={query} onChange={e=>setQuery(e.target.value)}/>
   {error?<p role="alert">{error} <button className="button small" onClick={()=>setRetry(v=>v+1)}>重试</button></p>:!room?<p role="status">正在读取群资料…</p>:<>
    <div className="room-members">{members.map(m=><div key={m.id} title={`${m.display_name||m.name} · ${m.id}`}><Avatar src={m.avatar} name={m.name}/><span>{m.display_name||m.name||m.id}</span></div>)}</div>
    <small className="muted">{query?`找到 ${members.length} 位成员`:`已获取 ${room.members.length} 位成员`}</small>
   </>}
   <section><h4>群聊名称</h4><p>{room?.name||contact.name}</p></section>
   <section><h4>群公告</h4><p className="room-announcement">{room?.notice||room?.announcement||(room?'暂无群公告':'正在读取…')}</p></section>
   <section><h4>备注 <button className="button small" onClick={()=>{setRemark(contact.remark);setEditing(!editing)}}>设置备注</button></h4>
    {editing?<form className="form-stack" onSubmit={async e=>{e.preventDefault();if(busy)return;setBusy(true);try{await act({kind:'remark',target:contact.id,content:remark});setEditing(false);notify('备注已保存')}catch(e){notify((e as Error).message)}finally{setBusy(false)}}}>
     <input aria-label="群备注" autoFocus value={remark} onChange={e=>setRemark(e.target.value)}/><div className="actions"><button type="button" className="button small" disabled={busy} onClick={()=>setEditing(false)}>取消</button><button className="button primary small" disabled={busy}>{busy?'保存中…':'保存备注'}</button></div>
    </form>:<p>{contact.remark||'群聊备注仅自己可见'}</p>}
   </section>
   <section><h4>我在本群的昵称</h4><p>{room?.self_name||'暂未获取'}</p><small className="muted">当前 core 尚不支持修改，请在微信中设置。</small></section>
  </div>
 </Modal>
}
