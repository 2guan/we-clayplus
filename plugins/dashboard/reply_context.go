package dashboard

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/we-clay/we-clay/plugins/ai"
	"github.com/we-clay/we-clay/plugins/contextcleaner"
)

// 回复证据还包括互动及引用摘要；它们变化时，旧生成结果同样失效。
// 长期记忆的来源哈希保持独立，避免改动已有记忆指纹。
func replyEvidenceHash(m Message) string {
	raw, _ := json.Marshal(struct {
		Base           MemorySource
		Source, Sender string
		Mentions       []string
		MentionAll     bool
		Interaction    *contextcleaner.Interaction
		Rich           *RichMessage
		Game           string
	}{memorySource(m), m.Source, m.Sender, m.Mentions, m.MentionAll, m.Interaction, m.Rich, messageGame(m)})
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}

func replyLocation(st State) *time.Location {
	loc, err := time.LoadLocation(settingText(st, "memory.timezone"))
	if err != nil {
		return time.Local
	}
	return loc
}

func contextAvailable(m Message) bool {
	if strings.HasPrefix(m.Game, "private:") || strings.HasPrefix(m.Source, "plugin:playroom-private:") {
		return false
	}
	if strings.HasPrefix(m.Status, "revoked") || slices.Contains([]string{"unknown", "sending", "failed", "deleted"}, m.Status) || m.Source == "guard" {
		return false
	}
	if m.Self && m.Status != "sent" {
		return false
	}
	return m.Kind != "system" || m.Interaction != nil && m.Interaction.Kind == "pat"
}

func messageGame(m Message) string {
	if m.Game != "" {
		return m.Game
	}
	if m.Source == "plugin:turtlesoup" {
		return "turtlesoup"
	}
	return ""
}

func contextMessage(st State, m Message) contextcleaner.Message {
	role, actor := "user", "participant"
	id := m.SenderID
	if m.Self {
		role, actor, id = "assistant", "self_operator", st.Account.ID
		if m.Source == "ai" {
			actor = "assistant_ai"
		}
	} else if memoryGroup(st, m.Target) {
		if id == m.Target {
			id = ""
		}
	} else if id == "" {
		id = m.Target
	}
	if m.Interaction != nil && m.Interaction.Kind == "pat" {
		actor = "interaction"
		id = m.Interaction.ActorID
	}
	name := m.Sender
	if id == "" {
		name = "未知发言人"
	}
	out := contextcleaner.Message{Game: messageGame(m), ID: m.ID, Session: m.Target, Role: role, SenderID: id, Sender: name, ActorType: actor, Source: m.Source, Kind: m.Kind, Text: m.Content, Time: m.Time, Status: m.Status, Mentions: append([]string(nil), m.Mentions...), MentionAll: m.MentionAll, Interaction: m.Interaction}
	if m.Kind == "image" {
		out.Text, out.MediaText = imageContextText(m), true
	}
	if m.Rich != nil && m.Rich.QuoteID != "" {
		out.Quote = &contextcleaner.Reference{ID: m.Rich.QuoteID, Sender: m.Rich.Author, Text: m.Rich.Quote}
		if m.Rich.QuoteUnavailable {
			out.Quote.Text = "[引用原消息已撤回或删除]"
			return out
		}
		for _, original := range st.Messages {
			if original.ID == m.Rich.QuoteID && original.Target == m.Target && !contextAvailable(original) {
				out.Quote.Text = "[引用原消息已撤回或不可用]"
				break
			}
			if original.ID == m.Rich.QuoteID && original.Target == m.Target && contextAvailable(original) {
				out.Quote.SenderID = original.SenderID
				out.Quote.Sender = original.Sender
				if original.Self {
					out.Quote.SenderID = st.Account.ID
					out.Quote.Sender = st.Account.Name
				}
				out.Quote.Text = "" // 已核实正文只在References中提供一次。
				out.Quote.Verified = true
				break
			}
		}
	}
	return out
}

// 只使用协议 @ 与同会话已核实的本人原消息，不按昵称或引用展示文字猜测指向。
func replyDirectedAtSelf(st State, m contextcleaner.Message) string {
	if st.Account.ID == "" {
		return ""
	}
	if slices.Contains(m.Mentions, st.Account.ID) {
		return "@ 本人"
	}
	if m.Quote != nil && m.Quote.Verified && m.Quote.SenderID == st.Account.ID {
		for _, original := range st.Messages {
			if original.ID == m.Quote.ID && original.Target == m.Session && original.Self && contextAvailable(original) {
				return "引用本人消息"
			}
		}
	}
	return ""
}

func buildReplyContext(st State, current Message, now time.Time) (ai.Conversation, []contextcleaner.Message) {
	loc := replyLocation(st)
	scene := "private"
	if memoryGroup(st, current.Target) {
		scene = "group"
	}
	c := ai.Conversation{AccountID: st.Account.ID, SelfName: st.Account.Name, Names: map[string]string{}, Session: current.Target, Scene: scene, Now: now.In(loc).Format(time.RFC3339), Timezone: loc.String(), Current: contextMessage(st, current)}
	for _, v := range st.Contacts {
		c.Names[v.ID] = v.Name
	}
	for _, v := range st.Messages {
		if v.Target == current.Target && v.SenderID != "" && v.SenderID != v.Target && !v.Self {
			c.Names[v.SenderID] = v.Sender
		}
	}
	retention := settingNumber(st, "memory.retention")
	if retention <= 0 {
		retention = 3000
	}
	cutoff := now.Add(-time.Duration(retention) * 24 * time.Hour)
	since, _ := time.Parse(time.RFC3339, settingOverride(st, current.Target, "memory_since"))
	if since.After(cutoff) {
		cutoff = since
	}
	seen := map[string]bool{current.ID: true}
	// 明确引用优先定位同一会话原消息；不跨会话按ID寻找。
	if c.Current.Quote != nil && c.Current.Quote.Verified {
		for i, m := range st.Messages {
			if m.ID != c.Current.Quote.ID || m.Target != current.Target {
				continue
			}
			// 必要引用排在首位，预算缩减优先移除相邻补充资料。
			if !seen[m.ID] && contextAvailable(m) {
				c.References = append(c.References, contextMessage(st, m))
				seen[m.ID] = true
			}
			for j := max(0, i-1); j <= min(len(st.Messages)-1, i+1); j++ {
				v := st.Messages[j]
				stamp, e := time.Parse(time.RFC3339, v.Time)
				if v.Target == current.Target && !seen[v.ID] && contextAvailable(v) && (since.IsZero() || e == nil && !stamp.Before(since)) {
					c.References = append(c.References, contextMessage(st, v))
					seen[v.ID] = true
				}
			}
			break
		}
	}
	// 只有明确历史提问才扩大时间搜索；普通闲聊不主动补数天旧记录。
	query := current.Content
	oldQuery := strings.Contains(query, "昨天") || strings.Contains(query, "前天") || strings.Contains(query, "上次") || strings.Contains(query, "上周")
	if oldQuery {
		words := strings.NewReplacer("昨天", " ", "前天", " ", "上次", " ", "上周", " ", "我们", " ", "你说", " ", "说过", " ", "记得", " ", "什么", " ", "？", " ", "?", " ").Replace(query)
		terms := strings.Fields(words)
		for i := len(st.Messages) - 1; i >= 0 && len(c.References) < 8; i-- {
			m := st.Messages[i]
			stamp, e := time.Parse(time.RFC3339, m.Time)
			if m.Target != current.Target || seen[m.ID] || !contextAvailable(m) || e != nil || !since.IsZero() && stamp.Before(since) {
				continue
			}
			timeMatch := false
			if strings.Contains(query, "昨天") {
				timeMatch = stamp.In(loc).Format("2006-01-02") == now.In(loc).AddDate(0, 0, -1).Format("2006-01-02")
			}
			if strings.Contains(query, "前天") {
				timeMatch = stamp.In(loc).Format("2006-01-02") == now.In(loc).AddDate(0, 0, -2).Format("2006-01-02")
			}
			if strings.Contains(query, "上周") {
				local := now.In(loc)
				daysSinceMonday := (int(local.Weekday()) + 6) % 7
				weekStart := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -daysSinceMonday)
				timeMatch = !stamp.Before(weekStart.AddDate(0, 0, -7)) && stamp.Before(weekStart)
			}
			matched := false
			for _, t := range terms {
				if len([]rune(t)) >= 2 && strings.Contains(m.Content, t) {
					matched = true
				}
			}
			if timeMatch || strings.Contains(query, "上次") && matched {
				c.References = append(c.References, contextMessage(st, m))
				seen[m.ID] = true
			}
		}
	}
	history := []contextcleaner.Message{}
	for _, m := range st.Messages {
		stamp, e := time.Parse(time.RFC3339, m.Time)
		if m.Target != current.Target || seen[m.ID] || !contextAvailable(m) || e != nil || stamp.Before(cutoff) {
			continue
		}
		history = append(history, contextMessage(st, m))
		seen[m.ID] = true
	}
	return c, history
}

func settingOverride(st State, target, key string) string {
	v, _ := st.Overrides[target][key].(string)
	return v
}

// 只判断微信真实 @ 元数据；同时 @ 本人、@所有人或手写昵称不属于仅 @ 他人。
func skipOtherGroupMention(st State, m Message) bool {
	if st.Settings["reply.skip_other_mentions"] == false || st.Account.ID == "" || m.MentionAll || !memoryGroup(st, m.Target) || slices.Contains(m.Mentions, st.Account.ID) {
		return false
	}
	return slices.ContainsFunc(m.Mentions, func(id string) bool { return id != "" && id != "notify@all" && id != "@all" })
}

// 在每次候选模型调用前复验来源与权限，故障切换也不使用旧账号或已撤回资料。
func (s *Server) refreshReplyContext(q ai.Request) (ai.Request, error) {
	if q.Conversation == nil {
		return q, nil
	}
	st := s.store.snapshot()
	if q.AccountID != st.Account.ID || q.AccountEpoch != st.AccountEpoch {
		return q, fmt.Errorf("账号已切换，停止旧会话回复")
	}
	current := q.Conversation.Current
	m := Message{ID: current.ID, Target: q.Target, SenderID: current.SenderID, Sender: current.Sender, Kind: current.Kind, Content: q.Prompt, Time: current.Time, Mentions: current.Mentions, MentionAll: current.MentionAll, Interaction: current.Interaction}
	found := false
	for _, v := range st.Messages {
		if v.ID == m.ID && v.Target == m.Target {
			m = v
			found = true
			break
		}
	}
	if !found || !contextAvailable(m) {
		return q, fmt.Errorf("当前消息已撤回或不可用")
	}
	if q.Source == "wechat" && skipOtherGroupMention(st, m) {
		return q, fmt.Errorf("消息仅 @ 其他成员，已停止自动回复")
	}
	conv, history := buildReplyContext(st, m, time.Now())
	if len(q.BatchIDs) > 1 {
		pending, err := pendingBatchMessages(st, q, m.ID)
		if err != nil {
			return q, err
		}
		conv.Pending = pending
	}
	if q.Source == "wechat-proactive" {
		conv, history = buildReplyContext(st, Message{Target: q.Target}, time.Now())
		conv.Current = contextMessage(st, m)
	}
	if q.InputKind != "" {
		conv.Current.Text = q.Prompt
	}
	q.Conversation = &conv
	q.Context = history
	trimBatchContext(st, &q)
	if q.Source != "wechat-proactive" && memoryExplicitPrefix(m.Content) {
		conv.MemoryWrite = "not_saved"
		items, err := s.memoryList(st.Account.ID, MemoryFilter{Target: m.Target})
		if err != nil {
			return q, err
		}
		for _, item := range items {
			for _, src := range item.Sources {
				if src.MessageID == m.ID && src.Target == m.Target {
					conv.MemoryWrite = item.Status
					break
				}
			}
		}
	}
	q.EvidenceHashes = map[string]string{}
	for _, v := range st.Messages {
		if v.Target == m.Target {
			q.EvidenceHashes[v.ID] = replyEvidenceHash(v)
		}
	}
	if settingFlag(st, "memory.enabled") && st.Overrides[m.Target]["memory_enabled"] == true {
		memorySpeaker := m
		if q.Source == "wechat-proactive" {
			memorySpeaker = proactiveMemorySpeaker(st, m)
		}
		items, err := s.replyMemories(st.Account.ID, memorySpeaker, 8)
		if err != nil {
			return q, err
		}
		for _, v := range items {
			sources := []string{}
			for _, src := range v.Sources {
				sources = append(sources, src.Target+":"+src.MessageID)
			}
			conv.Memories = append(conv.Memories, ai.MemoryEvidence{ID: v.ID, Version: v.Version, Scope: v.Scope, Subject: v.Subject, Target: v.Target, Content: v.Content, ConfirmedAt: v.ConfirmedAt, ExpiresAt: v.ExpiresAt, Sources: sources})
		}
	}
	// 历史和记忆是新选出的副本，进入实际调用前重新经过防护。
	return s.prepareSafety(q)
}

func (s *Server) validateReplyEvidence(q ai.Request) error {
	if q.Conversation == nil {
		return nil
	}
	st := s.store.snapshot()
	if st.Account.ID != q.AccountID || q.AccountEpoch != st.AccountEpoch {
		return fmt.Errorf("账号已切换，回复结果已丢弃")
	}
	if q.Source == "wechat" && skipOtherGroupMention(st, Message{Target: q.Target, Mentions: q.Conversation.Current.Mentions, MentionAll: q.Conversation.Current.MentionAll}) {
		return fmt.Errorf("消息仅 @ 其他成员，回复结果已丢弃")
	}
	ids := []string{q.Conversation.Current.ID}
	for _, v := range q.Conversation.Pending {
		if q.Source == "wechat" && skipOtherGroupMention(st, Message{Target: q.Target, Mentions: v.Mentions, MentionAll: v.MentionAll}) {
			return fmt.Errorf("合并消息仅 @ 其他成员，回复结果已丢弃")
		}
		ids = append(ids, v.ID)
	}
	for _, v := range q.Context {
		ids = append(ids, v.ID)
	}
	for _, v := range q.Conversation.References {
		ids = append(ids, v.ID)
	}
	for _, id := range ids {
		found := false
		for _, m := range st.Messages {
			if m.ID == id && m.Target == q.Target {
				found = contextAvailable(m) && q.EvidenceHashes[id] == replyEvidenceHash(m)
				break
			}
		}
		if !found {
			return fmt.Errorf("回复依据已撤回、删除或变化，结果已丢弃")
		}
	}
	if len(q.Conversation.Memories) > 0 {
		if !settingFlag(st, "memory.enabled") || st.Overrides[q.Target]["memory_enabled"] != true {
			return fmt.Errorf("记忆使用已关闭，回复结果已丢弃")
		}
		current := Message{Target: q.Target, SenderID: q.Conversation.Current.SenderID, Content: q.Prompt}
		if q.Source == "wechat-proactive" {
			current = proactiveMemorySpeaker(st, current)
			current.Content = q.Conversation.Current.Text
		}
		items, err := s.replyMemories(q.AccountID, current, 8)
		if err != nil {
			return err
		}
		for _, used := range q.Conversation.Memories {
			if !slices.ContainsFunc(items, func(v MemoryItem) bool { return v.ID == used.ID && v.Version == used.Version }) {
				return fmt.Errorf("采用的记忆已失效或变化，回复结果已丢弃")
			}
		}
	}
	return nil
}

type replyTrace struct {
	ToolNames      []string                `json:"tool_names,omitempty"`
	OutputModes    []string                `json:"output_modes,omitempty"`
	BatchIDs       []string                `json:"batch_ids,omitempty"`
	CurrentID      string                  `json:"current_id"`
	HistoryIDs     []string                `json:"history_ids"`
	ReferenceIDs   []string                `json:"reference_ids"`
	MemoryIDs      []string                `json:"memory_ids"`
	MemoryVersions []int                   `json:"memory_versions"`
	Model          string                  `json:"model"`
	Changes        []contextcleaner.Change `json:"changes"`
}

func (s *Server) saveReplyTrace(id string, q ai.Request, model string, changes []contextcleaner.Change) error {
	if q.Conversation == nil {
		return nil
	}
	v := replyTrace{BatchIDs: slices.Clone(q.BatchIDs), CurrentID: q.Conversation.Current.ID, Model: model, Changes: changes, HistoryIDs: []string{}, ReferenceIDs: []string{}, MemoryIDs: []string{}, MemoryVersions: []int{}}
	for _, tool := range q.Tools {
		v.ToolNames = append(v.ToolNames, tool.Name)
	}
	v.OutputModes = []string{"text"}
	st := s.store.snapshot()
	voice, image := replyOutputCapabilities(st, q.Prompt, q.InputKind)
	if !q.PlainReply && q.Source != "wechat-proactive" {
		if voice {
			v.OutputModes = append(v.OutputModes, "voice")
		}
		if image {
			v.OutputModes = append(v.OutputModes, "image")
		}
	}
	if settingText(st, "logging.level") == "DEBUG" {
		s.debugRecord(q.AccountID, q.AccountEpoch, q.Target, "本轮实际下发能力", map[string]any{"task": id, "model": model, "tool_names": v.ToolNames, "output_modes": v.OutputModes})
	}
	for _, m := range q.Context {
		v.HistoryIDs = append(v.HistoryIDs, m.ID)
	}
	for _, m := range q.Conversation.References {
		v.ReferenceIDs = append(v.ReferenceIDs, m.ID)
	}
	for _, m := range q.Conversation.Memories {
		v.MemoryIDs = append(v.MemoryIDs, m.ID)
		v.MemoryVersions = append(v.MemoryVersions, m.Version)
	}
	raw, _ := json.Marshal(v)
	return s.store.putSecret("reply-trace:"+q.AccountID+":"+id, string(raw))
}

func (s *Server) replyTraceAPI(w http.ResponseWriter, r *http.Request, u User) {
	if requireAdmin(u) != nil {
		writeError(w, 403, "仅管理员可查看回复依据")
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, 405, "请求方法不支持")
		return
	}
	account := s.store.snapshot().Account.ID
	raw, err := s.store.getSecret("reply-trace:" + account + ":" + r.URL.Query().Get("task"))
	var v replyTrace
	if err != nil || json.Unmarshal([]byte(raw), &v) != nil {
		writeError(w, 404, "该任务没有可用的回复依据")
		return
	}
	if s.store.snapshot().Account.ID != account {
		writeError(w, 409, "账号已切换")
		return
	}
	writeJSON(w, 200, v)
}
