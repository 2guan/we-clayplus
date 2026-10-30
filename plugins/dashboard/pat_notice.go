package dashboard

import "strings"

// 只从协议绑定的ID、通讯录及同会话的发言人元数据补姓名，不从正文猜人，
// 不为每条拍一拍发起群成员网络查询。
func patNoticeNames(st State, notices []Message) map[string]string {
	wanted := map[string]map[string]bool{}
	for _, m := range notices {
		if m.Kind != "system" || m.Interaction == nil || m.Interaction.Kind != "pat" {
			continue
		}
		if wanted[m.Target] == nil {
			wanted[m.Target] = map[string]bool{}
		}
		wanted[m.Target][m.Interaction.ActorID] = true
		wanted[m.Target][m.Interaction.TargetID] = true
	}
	if len(wanted) == 0 {
		return nil
	}
	contacts := map[string]Contact{}
	for _, c := range st.Contacts {
		contacts[c.ID] = c
	}
	names := map[string]string{}
	for target, ids := range wanted {
		for id := range ids {
			key := target + "\x00" + id
			if id != "" && id == st.Account.ID {
				names[key] = "我"
			} else if c := contacts[id]; c.Remark != "" && c.Remark != id {
				names[key] = c.Remark
			}
		}
	}
	for i := len(st.Messages) - 1; i >= 0; i-- {
		m := st.Messages[i]
		if !wanted[m.Target][m.SenderID] || m.SenderID == "" || m.SenderID == m.Target || m.Kind == "system" || m.Self {
			continue
		}
		key := m.Target + "\x00" + m.SenderID
		if names[key] == "" && m.Sender != "" && m.Sender != m.SenderID {
			names[key] = m.Sender
		}
	}
	for target, ids := range wanted {
		for id := range ids {
			key := target + "\x00" + id
			if c := contacts[id]; names[key] == "" && c.Name != "" && c.Name != id {
				names[key] = c.Name
			}
		}
	}
	return names
}

func resolvePatNotice(m Message, names map[string]string) Message {
	i := *m.Interaction // 只改展示副本，保留协议ID与核实状态。
	m.Interaction = &i
	if name := names[m.Target+"\x00"+i.ActorID]; name != "" {
		i.ActorName = name
	}
	if name := names[m.Target+"\x00"+i.TargetID]; name != "" {
		i.TargetName = name
	}
	label := func(id, name string) string {
		if name != "" {
			return name
		}
		if id != "" {
			return "未知成员（" + id + "）"
		}
		return "未知成员"
	}
	actor, target := label(i.ActorID, i.ActorName), label(i.TargetID, i.TargetName)
	if i.Template != "" {
		m.Content = noticeName.ReplaceAllStringFunc(i.Template, func(token string) string {
			id := strings.TrimSuffix(strings.TrimPrefix(token, "${"), "}")
			if id != "" && id == i.ActorID {
				return actor
			}
			if id != "" && id == i.TargetID {
				return target
			}
			return token // 自定义后缀里的其他占位文字不冒充参与者。
		})
	} else {
		// 兼容旧归档已丢失占位符的“对方”表头，只修复两位参与者，
		// 后缀中同样的文字、引号、表情与换行保持原样。
		before, after, ok := strings.Cut(m.Content, "拍了拍")
		if ok {
			for _, token := range []string{`"对方"`, "“对方”", "对方"} {
				if strings.TrimSpace(before) == token {
					before = strings.Replace(before, "对方", actor, 1)
					break
				}
			}
			trimmed := strings.TrimLeft(after, " \t\r\n")
			for _, token := range []string{`"对方"`, "“对方”", "对方"} {
				if strings.HasPrefix(trimmed, token) {
					after = after[:len(after)-len(trimmed)] + strings.Replace(trimmed, "对方", target, 1)
					break
				}
			}
			m.Content = before + "拍了拍" + after
		}
	}
	return m
}

func resolvePatNoticeViews(st *State) {
	names := patNoticeNames(*st, st.Messages)
	if names == nil {
		return
	}
	for j, m := range st.Messages {
		if m.Kind == "system" && m.Interaction != nil && m.Interaction.Kind == "pat" {
			st.Messages[j] = resolvePatNotice(m, names)
		}
	}
}
