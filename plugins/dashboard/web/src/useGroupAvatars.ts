// 文件名：useGroupAvatars.ts
// 主要功能：当前群共用一次成员读取，为头像与 @ 选择提供数据。
// 作者：2guan
import { useEffect, useState } from "react";
import { request } from "./api";
export type GroupMember = {
  id: string;
  name: string;
  display_name?: string;
  remark?: string;
  avatar?: string;
};
export function useGroupAvatars(group: string) {
  const [loaded, setLoaded] = useState<{
    group: string;
    members: GroupMember[];
    error: string;
  }>({ group: "", members: [], error: "" });
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    if (!group) return;
    let disposed = false;
    setLoaded({ group: "", members: [], error: "" });
    // 成员查询与头像补齐共用，输入字符时不重复请求 core。
    request<{ members: GroupMember[] }>(
      `/api/room?id=${encodeURIComponent(group)}`,
    )
      .then((room) => {
        if (!disposed)
          setLoaded({ group, members: room.members || [], error: "" });
      })
      .catch((e) => {
        if (!disposed) setLoaded({ group, members: [], error: e.message });
      });
    return () => {
      disposed = true;
    };
  }, [group, retry]);
  const members = loaded.group === group ? loaded.members : [];
  return {
    members,
    avatars: Object.fromEntries(
      members.filter((m) => m.avatar).map((m) => [m.id, m.avatar!]),
    ),
    loading: !!group && loaded.group !== group,
    error: loaded.group === group ? loaded.error : "",
    retry: () => setRetry((v) => v + 1),
  };
}
