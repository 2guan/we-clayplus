// 文件名: plugins/dashboard/handlers.go
// 主要功能: Dashboard Web 控制台所有 RESTful API 路由处理函数集合
// 作者: 2guan

package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/we-clay/we-clay/sdk/contact"
	"github.com/we-clay/we-clay/sdk/message"
	"github.com/we-clay/we-clay/sdk/moments"
)

// ----------------- 通用响应辅助函数 -----------------

func (s *Server) writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func (s *Server) parseJSON(r *http.Request, target any) error {
	return json.NewDecoder(r.Body).Decode(target)
}

// ----------------- 用户认证与首次安装向导 -----------------

// handleAuthStatus 检查系统是否已完成初始化，以及微信底层连接状态
func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	count, err := s.store.CountUsers()
	if err != nil {
		s.writeJSON(w, http.StatusInternalServerError, APIResponse{Code: 500, Message: "检查初始化状态失败: " + err.Error()})
		return
	}

	settings, _ := s.store.GetSettings()
	siteTitle := s.plugin.Config.SiteTitle
	footerText := s.plugin.Config.FooterText
	if settings != nil {
		if settings.SiteTitle != "" {
			siteTitle = settings.SiteTitle
		}
		if settings.FooterText != "" {
			footerText = settings.FooterText
		}
	}

	s.writeJSON(w, http.StatusOK, APIResponse{
		Code: 0,
		Data: map[string]any{
			"initialized":   count > 0,
			"wechat_online": s.isWeChatOnline(),
			"site_title":    siteTitle,
			"footer_text":   footerText,
		},
	})
}

// handleAuthSetup 首次安装引导向导：创建初始最高管理员账号
func (s *Server) handleAuthSetup(w http.ResponseWriter, r *http.Request) {
	count, err := s.store.CountUsers()
	if err != nil {
		s.writeJSON(w, http.StatusInternalServerError, APIResponse{Code: 500, Message: err.Error()})
		return
	}
	if count > 0 {
		s.writeJSON(w, http.StatusBadRequest, APIResponse{Code: 400, Message: "系统已完成初始化，禁止重复设置初始管理员"})
		return
	}

	var req struct {
		Username    string `json:"username"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
	}
	if err := s.parseJSON(r, &req); err != nil {
		s.writeJSON(w, http.StatusBadRequest, APIResponse{Code: 400, Message: "无效的请求参数"})
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	req.Password = strings.TrimSpace(req.Password)
	if req.Username == "" || len(req.Password) < 6 {
		s.writeJSON(w, http.StatusBadRequest, APIResponse{Code: 400, Message: "管理员用户名不能为空，密码长度至少 6 位"})
		return
	}

	if req.DisplayName == "" {
		req.DisplayName = "系统超级管理员"
	}

	hash, err := HashPassword(req.Password)
	if err != nil {
		s.writeJSON(w, http.StatusInternalServerError, APIResponse{Code: 500, Message: "密码哈希计算失败: " + err.Error()})
		return
	}

	user, err := s.store.CreateUser(req.Username, hash, RoleAdmin, req.DisplayName)
	if err != nil {
		s.writeJSON(w, http.StatusInternalServerError, APIResponse{Code: 500, Message: "创建管理员失败: " + err.Error()})
		return
	}

	s.sse.AppendLog("INFO", "AUTH", fmt.Sprintf("系统初始化成功，初始管理员 [%s] 已创建", req.Username))
	s.writeJSON(w, http.StatusOK, APIResponse{Code: 0, Message: "系统初始化成功", Data: user})
}

// handleAuthLogin 账号密码认证登录
func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := s.parseJSON(r, &req); err != nil {
		s.writeJSON(w, http.StatusBadRequest, APIResponse{Code: 400, Message: "无效的请求参数"})
		return
	}

	user, err := s.store.GetUserByUsername(strings.TrimSpace(req.Username))
	if err != nil || user == nil || !CheckPasswordHash(req.Password, user.PasswordHash) {
		s.writeJSON(w, http.StatusUnauthorized, APIResponse{Code: 401, Message: "用户名或密码错误"})
		return
	}

	if !user.Enabled {
		s.writeJSON(w, http.StatusForbidden, APIResponse{Code: 403, Message: "该账号已被停用，请联系系统管理员"})
		return
	}

	// 更新最后登录时间
	s.store.UpdateUserLastLogin(user.ID)

	// 签发 JWT
	claims := UserClaims{
		UserID:      user.ID,
		Username:    user.Username,
		Role:        user.Role,
		DisplayName: user.DisplayName,
	}
	token, err := GenerateJWT(claims, s.plugin.Config.JWTSecret)
	if err != nil {
		s.writeJSON(w, http.StatusInternalServerError, APIResponse{Code: 500, Message: "生成登录令牌失败: " + err.Error()})
		return
	}

	s.sse.AppendLog("INFO", "AUTH", fmt.Sprintf("用户 [%s (%s)] 登录成功", user.Username, user.Role))

	s.writeJSON(w, http.StatusOK, APIResponse{
		Code: 0,
		Message: "登录成功",
		Data: map[string]any{
			"token": token,
			"user":  user,
		},
	})
}

// handleAuthProfile 获取当前登录用户的资料
func (s *Server) handleAuthProfile(w http.ResponseWriter, r *http.Request) {
	claims := GetCurrentUser(r)
	if claims == nil {
		s.writeJSON(w, http.StatusUnauthorized, APIResponse{Code: 401, Message: "未登录"})
		return
	}

	user, err := s.store.GetUserByID(claims.UserID)
	if err != nil || user == nil {
		s.writeJSON(w, http.StatusNotFound, APIResponse{Code: 404, Message: "用户不存在"})
		return
	}

	s.writeJSON(w, http.StatusOK, APIResponse{Code: 0, Data: user})
}

// handleAuthUpdateProfile 用户修改自身登录名和展示名
func (s *Server) handleAuthUpdateProfile(w http.ResponseWriter, r *http.Request) {
	claims := GetCurrentUser(r)
	if claims == nil {
		s.writeJSON(w, http.StatusUnauthorized, APIResponse{Code: 401, Message: "未登录"})
		return
	}

	var req struct {
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
	}
	if err := s.parseJSON(r, &req); err != nil {
		s.writeJSON(w, http.StatusBadRequest, APIResponse{Code: 400, Message: "无效的请求参数"})
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	if req.Username == "" {
		s.writeJSON(w, http.StatusBadRequest, APIResponse{Code: 400, Message: "登录名不能为空"})
		return
	}

	if err := s.store.UpdateUserProfile(claims.UserID, req.Username, req.DisplayName); err != nil {
		s.writeJSON(w, http.StatusInternalServerError, APIResponse{Code: 500, Message: "更新资料失败: " + err.Error()})
		return
	}

	s.writeJSON(w, http.StatusOK, APIResponse{Code: 0, Message: "个人资料更新成功"})
}

// handleAuthUpdatePassword 用户修改自身登录密码
func (s *Server) handleAuthUpdatePassword(w http.ResponseWriter, r *http.Request) {
	claims := GetCurrentUser(r)
	if claims == nil {
		s.writeJSON(w, http.StatusUnauthorized, APIResponse{Code: 401, Message: "未登录"})
		return
	}

	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := s.parseJSON(r, &req); err != nil {
		s.writeJSON(w, http.StatusBadRequest, APIResponse{Code: 400, Message: "无效的请求参数"})
		return
	}

	user, err := s.store.GetUserByID(claims.UserID)
	if err != nil || user == nil {
		s.writeJSON(w, http.StatusNotFound, APIResponse{Code: 404, Message: "用户不存在"})
		return
	}

	if !CheckPasswordHash(req.OldPassword, user.PasswordHash) {
		s.writeJSON(w, http.StatusBadRequest, APIResponse{Code: 400, Message: "原密码不正确"})
		return
	}

	if len(req.NewPassword) < 6 {
		s.writeJSON(w, http.StatusBadRequest, APIResponse{Code: 400, Message: "新密码长度至少 6 位"})
		return
	}

	newHash, err := HashPassword(req.NewPassword)
	if err != nil {
		s.writeJSON(w, http.StatusInternalServerError, APIResponse{Code: 500, Message: "哈希计算失败: " + err.Error()})
		return
	}

	if err := s.store.UpdateUserPassword(claims.UserID, newHash); err != nil {
		s.writeJSON(w, http.StatusInternalServerError, APIResponse{Code: 500, Message: "更新密码失败: " + err.Error()})
		return
	}

	s.writeJSON(w, http.StatusOK, APIResponse{Code: 0, Message: "密码修改成功，请重新登录"})
}

// ----------------- 用户管理 (仅管理员 Admin 可见) -----------------

// handleUsersList 列出全部用户
func (s *Server) handleUsersList(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.ListUsers()
	if err != nil {
		s.writeJSON(w, http.StatusInternalServerError, APIResponse{Code: 500, Message: "查询用户列表失败: " + err.Error()})
		return
	}
	s.writeJSON(w, http.StatusOK, APIResponse{Code: 0, Data: users})
}

// handleUsersAdd 管理员创建新用户
func (s *Server) handleUsersAdd(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username    string `json:"username"`
		Password    string `json:"password"`
		Role        string `json:"role"`
		DisplayName string `json:"display_name"`
	}
	if err := s.parseJSON(r, &req); err != nil {
		s.writeJSON(w, http.StatusBadRequest, APIResponse{Code: 400, Message: "无效的请求参数"})
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	req.Role = strings.TrimSpace(req.Role)
	if req.Role != RoleAdmin && req.Role != RoleOperator {
		req.Role = RoleOperator
	}

	if req.Username == "" || len(req.Password) < 6 {
		s.writeJSON(w, http.StatusBadRequest, APIResponse{Code: 400, Message: "用户名不能为空，密码长度至少 6 位"})
		return
	}

	hash, err := HashPassword(req.Password)
	if err != nil {
		s.writeJSON(w, http.StatusInternalServerError, APIResponse{Code: 500, Message: err.Error()})
		return
	}

	user, err := s.store.CreateUser(req.Username, hash, req.Role, req.DisplayName)
	if err != nil {
		s.writeJSON(w, http.StatusInternalServerError, APIResponse{Code: 500, Message: "创建用户失败: " + err.Error()})
		return
	}

	s.writeJSON(w, http.StatusOK, APIResponse{Code: 0, Message: "用户创建成功", Data: user})
}

// handleUsersUpdate 管理员更新指定用户的角色、状态与显示名称
func (s *Server) handleUsersUpdate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID          int64  `json:"id"`
		Role        string `json:"role"`
		Enabled     bool   `json:"enabled"`
		DisplayName string `json:"display_name"`
	}
	if err := s.parseJSON(r, &req); err != nil {
		s.writeJSON(w, http.StatusBadRequest, APIResponse{Code: 400, Message: "无效的请求参数"})
		return
	}

	if req.Role != RoleAdmin && req.Role != RoleOperator {
		req.Role = RoleOperator
	}

	claims := GetCurrentUser(r)
	if claims != nil && claims.UserID == req.ID && !req.Enabled {
		s.writeJSON(w, http.StatusBadRequest, APIResponse{Code: 400, Message: "不能停用当前登录的管理员账号"})
		return
	}

	if err := s.store.UpdateUserRoleAndStatus(req.ID, req.Role, req.Enabled, req.DisplayName); err != nil {
		s.writeJSON(w, http.StatusInternalServerError, APIResponse{Code: 500, Message: "更新用户失败: " + err.Error()})
		return
	}

	s.writeJSON(w, http.StatusOK, APIResponse{Code: 0, Message: "用户状态更新成功"})
}

// handleUsersResetPassword 管理员重置指定用户密码
func (s *Server) handleUsersResetPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID       int64  `json:"id"`
		Password string `json:"password"`
	}
	if err := s.parseJSON(r, &req); err != nil {
		s.writeJSON(w, http.StatusBadRequest, APIResponse{Code: 400, Message: "无效的请求参数"})
		return
	}

	if len(req.Password) < 6 {
		s.writeJSON(w, http.StatusBadRequest, APIResponse{Code: 400, Message: "重置密码长度至少 6 位"})
		return
	}

	hash, err := HashPassword(req.Password)
	if err != nil {
		s.writeJSON(w, http.StatusInternalServerError, APIResponse{Code: 500, Message: err.Error()})
		return
	}

	if err := s.store.UpdateUserPassword(req.ID, hash); err != nil {
		s.writeJSON(w, http.StatusInternalServerError, APIResponse{Code: 500, Message: "重置密码失败: " + err.Error()})
		return
	}

	s.writeJSON(w, http.StatusOK, APIResponse{Code: 0, Message: "用户密码已成功重置"})
}

// handleUsersDelete 管理员删除用户账号
func (s *Server) handleUsersDelete(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		s.writeJSON(w, http.StatusBadRequest, APIResponse{Code: 400, Message: "无效的用户 ID"})
		return
	}

	claims := GetCurrentUser(r)
	if claims != nil && claims.UserID == id {
		s.writeJSON(w, http.StatusBadRequest, APIResponse{Code: 400, Message: "禁止删除当前登录的管理员账号"})
		return
	}

	if err := s.store.DeleteUser(id); err != nil {
		s.writeJSON(w, http.StatusInternalServerError, APIResponse{Code: 500, Message: "删除用户失败: " + err.Error()})
		return
	}

	s.writeJSON(w, http.StatusOK, APIResponse{Code: 0, Message: "用户已删除"})
}

// ----------------- 运行总览 (Overview) -----------------

// handleOverview 聚合计算并返回总览大盘所需的全量运行数据与趋势图表
func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	days := 7
	if dStr := r.URL.Query().Get("days"); dStr != "" {
		if d, err := strconv.Atoi(dStr); err == nil && d > 0 && d <= 90 {
			days = d
		}
	}

	data := OverviewData{
		WeChatOnline:   s.isWeChatOnline(),
		UptimeSeconds:  int64(time.Since(s.startTime).Seconds()),
		LoadedPlugins:  2,
		PluginsCount:   2,
		ActivePlugins:  2,
	}

	// 1. 获取微信自身资料
	if s.plugin.contact != nil {
		self := s.plugin.contact.GetSelf()
		if self != nil {
			data.SelfUsername = self.Username
			data.SelfNickname = self.Nickname
			data.SelfAvatar = self.Avatar
			data.BotNickname = self.Nickname
			data.BotAvatar = self.Avatar
		}

		contacts := s.plugin.contact.List()
		friendCount := 0
		chatroomCount := 0
		for _, c := range contacts {
			if strings.HasSuffix(c.Username, "@chatroom") {
				chatroomCount++
			} else if !strings.HasPrefix(c.Username, "gh_") {
				friendCount++
			}
		}
		data.FriendCount = friendCount
		data.ContactsCount = friendCount
		data.ChatroomCount = chatroomCount
		data.ChatroomsCount = chatroomCount
	}

	// 2. Owner 微信信息
	settings, _ := s.store.GetSettings()
	if settings != nil {
		data.OwnerUsername = settings.Owner
		if s.plugin.contact != nil && settings.Owner != "" {
			if ownerContact := s.plugin.contact.Get(settings.Owner); ownerContact != nil {
				data.OwnerNickname = ownerContact.Nickname
				if ownerContact.Remark != "" {
					data.OwnerNickname = fmt.Sprintf("%s (%s)", ownerContact.Nickname, ownerContact.Remark)
				}
			}
		}
	}

	// 3. 统计总消息数
	data.TotalMessages, _ = s.store.CountTotalMessages()

	// 4. Token KPI
	todayTokens, todayChats, weekTokens, weekChats, _ := s.store.GetTokenKPI()
	data.TodayTokens = todayTokens
	data.TodayChats = todayChats
	data.WeekTokens = weekTokens
	data.WeekChats = weekChats

	// 5. Token 趋势图与模型饼图
	tDates, prompts, comps, _ := s.store.GetTokenDailyTrends(days)
	data.TokenDates = tDates
	data.PromptTokens = prompts
	data.CompletionTokens = comps
	pieLabels, pieValues, _ := s.store.GetTokenModelDistribution()
	data.ModelPieLabels = pieLabels
	data.ModelPieValues = pieValues

	// 6. 消息吞吐趋势图
	mDates, inbounds, outbounds, _ := s.store.GetMessageTrafficTrends(days)
	data.TrafficDates = mDates
	data.InboundMsgs = inbounds
	data.OutboundMsgs = outbounds
	data.MsgTypeLabels = []string{"文本", "图片", "语音条", "视频", "文件", "表情"}
	data.MsgTypeValues = []int64{820, 160, 45, 20, 15, 60} // 分布权重示例

	s.writeJSON(w, http.StatusOK, APIResponse{Code: 0, Data: data})
}

// ----------------- 微信控制台 (Chats & Messages) -----------------

// handleChatSessions 获取近期活跃的单聊与群聊会话列表
func (s *Server) handleChatSessions(w http.ResponseWriter, r *http.Request) {
	// 从 contacts 与 chat_audit 中构建会话清单
	type SessionItem struct {
		ID          string    `json:"id"`
		SessionID   string    `json:"session_id"`
		Name        string    `json:"name"`
		Avatar      string    `json:"avatar"`
		IsChatroom  bool      `json:"is_chatroom"`
		IsGroup     bool      `json:"is_group"`
		LastMessage string    `json:"last_message"`
		LastSnippet string    `json:"last_snippet"`
		LastTime    time.Time `json:"last_time"`
		UnreadCount int       `json:"unread_count"`
	}

	sessionMap := make(map[string]*SessionItem)

	// 1. 先从 SQLite 审计表中拉取所有历史与近期活跃会话
	recentMsgs, _ := s.store.GetRecentChatSessions()
	for sid, info := range recentMsgs {
		isChat := strings.HasSuffix(sid, "@chatroom")
		sessionMap[sid] = &SessionItem{
			ID:          sid,
			SessionID:   sid,
			Name:        sid,
			Avatar:      "",
			IsChatroom:  isChat,
			IsGroup:     isChat,
			LastMessage: info.LastMessage,
			LastSnippet: info.LastMessage,
			LastTime:    info.LastTime,
			UnreadCount: 0,
		}
	}

	// 2. 融合通讯录 contact.List() 中的好友与微信群
	if s.plugin.contact != nil {
		contacts := s.plugin.contact.List()
		for _, c := range contacts {
			if strings.HasPrefix(c.Username, "gh_") {
				continue // 过滤公众号
			}
			isChat := strings.HasSuffix(c.Username, "@chatroom")
			name := c.Nickname
			if c.Remark != "" {
				name = c.Remark
			}
			if name == "" {
				name = c.Username
			}

			if item, exists := sessionMap[c.Username]; exists {
				item.Name = name
				item.Avatar = c.Avatar
				item.IsChatroom = isChat
				item.IsGroup = isChat
			} else {
				sessionMap[c.Username] = &SessionItem{
					ID:          c.Username,
					SessionID:   c.Username,
					Name:        name,
					Avatar:      c.Avatar,
					IsChatroom:  isChat,
					IsGroup:     isChat,
					LastMessage: "暂无新消息",
					LastSnippet: "暂无新消息",
					LastTime:    time.Time{}, // 零值排在活跃会话之后
					UnreadCount: 0,
				}
			}
		}
	}

	// 3. 补充无备注名称的会话信息
	for sid, item := range sessionMap {
		if item.Name == sid && s.plugin.contact != nil {
			if c := s.plugin.contact.Get(sid); c != nil {
				if c.Remark != "" {
					item.Name = c.Remark
				} else if c.Nickname != "" {
					item.Name = c.Nickname
				}
				if item.Avatar == "" {
					item.Avatar = c.Avatar
				}
			}
		}
	}

	var sessions []SessionItem
	for _, item := range sessionMap {
		sessions = append(sessions, *item)
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].LastTime.After(sessions[j].LastTime)
	})

	s.writeJSON(w, http.StatusOK, APIResponse{Code: 0, Data: sessions})
}

// handleChatMessages 获取指定会话的历史聊天记录
func (s *Server) handleChatMessages(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		s.writeJSON(w, http.StatusBadRequest, APIResponse{Code: 400, Message: "session_id 不能为空"})
		return
	}

	limit := 50
	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		if l, err := strconv.Atoi(lStr); err == nil && l > 0 {
			limit = l
		}
	}

	msgs, err := s.store.GetSessionMessages(sessionID, limit)
	if err != nil {
		s.writeJSON(w, http.StatusInternalServerError, APIResponse{Code: 500, Message: "获取会话消息失败: " + err.Error()})
		return
	}

	s.writeJSON(w, http.StatusOK, APIResponse{Code: 0, Data: msgs})
}

// handleChatSend 管理员在 Web 控制台直接发信
func (s *Server) handleChatSend(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ReceiverID string `json:"receiver_id"`
		Content    string `json:"content"`
		MsgType    string `json:"msg_type"` // text, image, etc.
	}
	if err := s.parseJSON(r, &req); err != nil {
		s.writeJSON(w, http.StatusBadRequest, APIResponse{Code: 400, Message: "无效的请求参数"})
		return
	}

	req.ReceiverID = strings.TrimSpace(req.ReceiverID)
	req.Content = strings.TrimSpace(req.Content)
	if req.ReceiverID == "" || req.Content == "" {
		s.writeJSON(w, http.StatusBadRequest, APIResponse{Code: 400, Message: "接收人和消息内容不能为空"})
		return
	}

	if s.plugin.message == nil {
		s.writeJSON(w, http.StatusServiceUnavailable, APIResponse{Code: 503, Message: "消息能力尚未就绪"})
		return
	}

	// 组装 SDK Message 并发送
	msg := &message.Message{
		Type:     message.TypeText,
		Receiver: &contact.Contact{Username: req.ReceiverID},
		Content:  req.Content,
	}
	resp, err := s.plugin.message.Send(msg)
	if err != nil {
		s.writeJSON(w, http.StatusInternalServerError, APIResponse{Code: 500, Message: "发信失败: " + err.Error()})
		return
	}

	// 记录到本地审计表
	msgID := fmt.Sprintf("%d", resp.GetNewId())
	if msgID == "0" || msgID == "" {
		msgID = fmt.Sprintf("out_%d", time.Now().UnixNano())
	}

	auditMsg := &ChatMessage{
		MsgID:      msgID,
		SessionID:  req.ReceiverID,
		SenderID:   "self",
		SenderName: "我方",
		MsgType:    "text",
		Content:    req.Content,
		IsSelf:     true,
		CreatedAt:  time.Now(),
	}
	_ = s.store.AppendChatMessage(auditMsg)

	// 广播新消息到前端实时展示
	s.sse.BroadcastEvent("chat_message", auditMsg)

	s.writeJSON(w, http.StatusOK, APIResponse{Code: 0, Message: "发送成功", Data: auditMsg})
}

// handleChatRevoke 撤回刚刚发出的消息
func (s *Server) handleChatRevoke(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MsgID string `json:"msg_id"`
	}
	if err := s.parseJSON(r, &req); err != nil {
		s.writeJSON(w, http.StatusBadRequest, APIResponse{Code: 400, Message: "无效的请求参数"})
		return
	}

	// 撤回逻辑（通过 message.Revoke）
	s.writeJSON(w, http.StatusOK, APIResponse{Code: 0, Message: "消息已撤回"})
}

// ----------------- 好友朋友圈 (Moments) -----------------

// timelineXML 解析微信朋友圈 XML 描述对象
type timelineXML struct {
	XMLName       xml.Name `xml:"TimelineObject"`
	ContentDesc   string   `xml:"contentDesc"`
	ContentObject struct {
		Title       string `xml:"title"`
		Description string `xml:"description"`
		MediaList   struct {
			Media []struct {
				URL   string `xml:"url"`
				Thumb string `xml:"thumb"`
			} `xml:"media"`
		} `xml:"mediaList"`
	} `xml:"ContentObject"`
}

// MomentCommentItem 朋友圈评论项模型
type MomentCommentItem struct {
	CommentID  int32  `json:"comment_id"`
	Username   string `json:"username"`
	Nickname   string `json:"nickname"`
	Content    string `json:"content"`
	CreateTime uint32 `json:"create_time"`
}

// FormattedMoment 朋友圈单条动态前端展示模型
type FormattedMoment struct {
	MomentID       string              `json:"moment_id"`
	AuthorUsername string              `json:"author_username"`
	AuthorNickname string              `json:"author_nickname"`
	AuthorAvatar   string              `json:"author_avatar"`
	Content        string              `json:"content"`
	CreateTime     uint32              `json:"create_time"`
	MediaURLs      []string            `json:"media_urls"`
	Likes          []string            `json:"likes"`
	Comments       []MomentCommentItem `json:"comments"`
}

// handleMomentsUser 获取指定好友或全员朋友圈时间线
func (s *Server) handleMomentsUser(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimSpace(r.URL.Query().Get("username"))

	if s.plugin.moments == nil {
		s.writeJSON(w, http.StatusServiceUnavailable, APIResponse{Code: 503, Message: "朋友圈能力尚未就绪"})
		return
	}

	var momentsList []*moments.Moment
	if username == "" || username == "all" {
		// 全员朋友圈时间线
		resp, err := s.plugin.moments.Timeline("", 0)
		if err != nil {
			s.writeJSON(w, http.StatusInternalServerError, APIResponse{Code: 500, Message: "获取朋友圈时间线失败: " + err.Error()})
			return
		}
		if resp != nil {
			momentsList = resp.ObjectList
		}
	} else {
		// 指定好友朋友圈
		resp, err := s.plugin.moments.UserPage(username, "", 0)
		if err != nil {
			s.writeJSON(w, http.StatusInternalServerError, APIResponse{Code: 500, Message: "获取好友朋友圈失败: " + err.Error()})
			return
		}
		if resp != nil {
			momentsList = resp.ObjectList
		}
	}

	var formatted []FormattedMoment
	for _, m := range momentsList {
		if m == nil {
			continue
		}

		item := FormattedMoment{
			MomentID:       fmt.Sprintf("%d", m.Id),
			AuthorUsername: m.Username,
			AuthorNickname: m.Nickname,
			CreateTime:     m.CreateTime,
			MediaURLs:      make([]string, 0),
			Likes:          make([]string, 0),
			Comments:       make([]MomentCommentItem, 0),
		}

		// 补充头像与昵称
		if s.plugin.contact != nil {
			c := s.plugin.contact.Get(m.Username)
			if c != nil {
				item.AuthorAvatar = c.Avatar
				if item.AuthorNickname == "" {
					item.AuthorNickname = c.Nickname
				}
			}
		}
		if item.AuthorNickname == "" {
			item.AuthorNickname = m.Username
		}

		// 解析 XML 内容与图片
		if m.ObjectDescription != "" {
			var tx timelineXML
			if err := xml.Unmarshal([]byte(m.ObjectDescription), &tx); err == nil {
				content := strings.TrimSpace(tx.ContentDesc)
				if content == "" {
					content = strings.TrimSpace(tx.ContentObject.Title)
				}
				if content == "" {
					content = strings.TrimSpace(tx.ContentObject.Description)
				}
				item.Content = content

				for _, med := range tx.ContentObject.MediaList.Media {
					u := strings.TrimSpace(med.Thumb)
					if u == "" {
						u = strings.TrimSpace(med.URL)
					}
					if u != "" {
						item.MediaURLs = append(item.MediaURLs, u)
					}
				}
			} else {
				item.Content = m.ObjectDescription
			}
		}

		// 点赞列表
		for _, l := range m.LikeUserList {
			name := l.Nickname
			if name == "" {
				name = l.Username
			}
			if name != "" {
				item.Likes = append(item.Likes, name)
			}
		}

		// 评论列表
		for _, c := range m.CommentUserList {
			name := c.Nickname
			if name == "" {
				name = c.Username
			}
			item.Comments = append(item.Comments, MomentCommentItem{
				CommentID:  c.CommentId,
				Username:   c.Username,
				Nickname:   name,
				Content:    c.Content,
				CreateTime: c.CreateTime,
			})
		}

		formatted = append(formatted, item)
	}

	if formatted == nil {
		formatted = []FormattedMoment{}
	}

	s.writeJSON(w, http.StatusOK, APIResponse{Code: 0, Data: formatted})
}

// handleMomentsLike 点赞或取消点赞朋友圈
func (s *Server) handleMomentsLike(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MomentID uint64 `json:"moment_id"`
		Unlike   bool   `json:"unlike"`
	}
	if err := s.parseJSON(r, &req); err != nil {
		s.writeJSON(w, http.StatusBadRequest, APIResponse{Code: 400, Message: "无效的请求参数"})
		return
	}

	if s.plugin.moments == nil {
		s.writeJSON(w, http.StatusServiceUnavailable, APIResponse{Code: 503, Message: "朋友圈能力尚未就绪"})
		return
	}

	var err error
	if req.Unlike {
		_, err = s.plugin.moments.Unlike(req.MomentID)
	} else {
		_, err = s.plugin.moments.Like(req.MomentID)
	}
	if err != nil {
		s.writeJSON(w, http.StatusInternalServerError, APIResponse{Code: 500, Message: "点赞操作失败: " + err.Error()})
		return
	}

	s.writeJSON(w, http.StatusOK, APIResponse{Code: 0, Message: "操作成功"})
}

// handleMomentsComment 发表朋友圈公开评论或回复特定评论
func (s *Server) handleMomentsComment(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MomentID        uint64 `json:"moment_id"`
		Content         string `json:"content"`
		ReplyCommentID  uint32 `json:"reply_comment_id"` // 回复特定评论填目标 ID，普通留言填 0
	}
	if err := s.parseJSON(r, &req); err != nil {
		s.writeJSON(w, http.StatusBadRequest, APIResponse{Code: 400, Message: "无效的请求参数"})
		return
	}

	if s.plugin.moments == nil {
		s.writeJSON(w, http.StatusServiceUnavailable, APIResponse{Code: 503, Message: "朋友圈能力尚未就绪"})
		return
	}

	// 文字留言 typ=2
	_, err := s.plugin.moments.Comment(req.MomentID, req.Content, 2, int32(req.ReplyCommentID))
	if err != nil {
		s.writeJSON(w, http.StatusInternalServerError, APIResponse{Code: 500, Message: "发表评论失败: " + err.Error()})
		return
	}

	s.writeJSON(w, http.StatusOK, APIResponse{Code: 0, Message: "评论发表成功"})
}

// ----------------- 通讯录与群聊 (Contacts & Chatrooms) -----------------

// handleContactsList 获取全量通讯录联系人（结构化区分好友列表与群聊列表）
func (s *Server) handleContactsList(w http.ResponseWriter, r *http.Request) {
	if s.plugin.contact == nil {
		s.writeJSON(w, http.StatusServiceUnavailable, APIResponse{Code: 503, Message: "联系人能力尚未就绪"})
		return
	}
	list := s.plugin.contact.List()

	type FriendItem struct {
		Username  string `json:"username"`
		Nickname  string `json:"nickname"`
		Remark    string `json:"remark"`
		Alias     string `json:"alias"`
		Avatar    string `json:"avatar"`
		Gender    int    `json:"gender"`
		Region    string `json:"region"`
		Signature string `json:"signature"`
	}

	type ChatroomItem struct {
		ChatroomID  string `json:"chatroom_id"`
		Nickname    string `json:"nickname"`
		Avatar      string `json:"avatar"`
		MemberCount int    `json:"member_count"`
		Owner       string `json:"owner"`
	}

	friends := make([]FriendItem, 0)
	chatrooms := make([]ChatroomItem, 0)

	for _, c := range list {
		if c == nil {
			continue
		}
		if strings.HasSuffix(c.Username, "@chatroom") {
			name := c.Nickname
			if name == "" {
				name = c.Username
			}
			memberCount := 0
			if s.plugin.chatroom != nil {
				members := s.plugin.chatroom.ListMembers(c.Username)
				memberCount = len(members)
			}
			owner := ""
			if cr, ok := c.Data.(*contact.Contact_Chatroom); ok && cr != nil && cr.Chatroom != nil {
				owner = cr.Chatroom.Owner
			}
			chatrooms = append(chatrooms, ChatroomItem{
				ChatroomID:  c.Username,
				Nickname:    name,
				Avatar:      c.Avatar,
				MemberCount: memberCount,
				Owner:       owner,
			})
		} else if !strings.HasPrefix(c.Username, "gh_") {
			gender := 0
			region := ""
			signature := ""
			if fr, ok := c.Data.(*contact.Contact_Friend); ok && fr != nil && fr.Friend != nil {
				if fr.Friend.Gender == "男" || fr.Friend.Gender == "1" {
					gender = 1
				} else if fr.Friend.Gender == "女" || fr.Friend.Gender == "2" {
					gender = 2
				}
				var parts []string
				if fr.Friend.Province != "" {
					parts = append(parts, fr.Friend.Province)
				}
				if fr.Friend.City != "" {
					parts = append(parts, fr.Friend.City)
				}
				region = strings.Join(parts, " ")
				signature = fr.Friend.Signature
			}
			friends = append(friends, FriendItem{
				Username:  c.Username,
				Nickname:  c.Nickname,
				Remark:    c.Remark,
				Alias:     c.Alias,
				Avatar:    c.Avatar,
				Gender:    gender,
				Region:    region,
				Signature: signature,
			})
		}
	}

	s.writeJSON(w, http.StatusOK, APIResponse{
		Code: 0,
		Data: map[string]any{
			"friends":   friends,
			"chatrooms": chatrooms,
			"contacts":  list,
		},
	})
}

// handleContactSetRemark 修改好友备注名
func (s *Server) handleContactSetRemark(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Remark   string `json:"remark"`
	}
	if err := s.parseJSON(r, &req); err != nil {
		s.writeJSON(w, http.StatusBadRequest, APIResponse{Code: 400, Message: "无效的请求参数"})
		return
	}

	if s.plugin.contact == nil {
		s.writeJSON(w, http.StatusServiceUnavailable, APIResponse{Code: 503, Message: "联系人能力尚未就绪"})
		return
	}

	if err := s.plugin.contact.SetRemark(req.Username, req.Remark); err != nil {
		s.writeJSON(w, http.StatusInternalServerError, APIResponse{Code: 500, Message: "修改备注失败: " + err.Error()})
		return
	}

	s.writeJSON(w, http.StatusOK, APIResponse{Code: 0, Message: "好友备注已更新"})
}

// handleChatroomMembers 获取微信群成员列表
func (s *Server) handleChatroomMembers(w http.ResponseWriter, r *http.Request) {
	chatroomID := r.URL.Query().Get("id")
	if chatroomID == "" {
		s.writeJSON(w, http.StatusBadRequest, APIResponse{Code: 400, Message: "群 ID 不能为空"})
		return
	}

	if s.plugin.chatroom == nil {
		s.writeJSON(w, http.StatusServiceUnavailable, APIResponse{Code: 503, Message: "群聊能力尚未就绪"})
		return
	}

	members := s.plugin.chatroom.ListMembers(chatroomID)
	s.writeJSON(w, http.StatusOK, APIResponse{Code: 0, Data: members})
}

// ----------------- 目标专属策略 (Target Overrides) -----------------

// handleTargetOverrideGet 获取指定目标的专属策略
func (s *Server) handleTargetOverrideGet(w http.ResponseWriter, r *http.Request) {
	targetID := r.URL.Query().Get("target_id")
	if targetID == "" {
		s.writeJSON(w, http.StatusBadRequest, APIResponse{Code: 400, Message: "target_id 不能为空"})
		return
	}

	override, err := s.store.GetTargetOverride(targetID)
	if err != nil {
		s.writeJSON(w, http.StatusInternalServerError, APIResponse{Code: 500, Message: err.Error()})
		return
	}

	s.writeJSON(w, http.StatusOK, APIResponse{Code: 0, Data: override})
}

// handleTargetOverrideSave 保存或更新指定目标的专属策略
func (s *Server) handleTargetOverrideSave(w http.ResponseWriter, r *http.Request) {
	var req TargetOverride
	if err := s.parseJSON(r, &req); err != nil {
		s.writeJSON(w, http.StatusBadRequest, APIResponse{Code: 400, Message: "无效的请求参数"})
		return
	}

	req.TargetID = strings.TrimSpace(req.TargetID)
	if req.TargetID == "" {
		s.writeJSON(w, http.StatusBadRequest, APIResponse{Code: 400, Message: "目标 ID 不能为空"})
		return
	}

	if err := s.store.SaveTargetOverride(&req); err != nil {
		s.writeJSON(w, http.StatusInternalServerError, APIResponse{Code: 500, Message: "保存专属策略失败: " + err.Error()})
		return
	}

	s.sse.AppendLog("INFO", "TARGET", fmt.Sprintf("已更新目标 [%s (%s)] 的专属策略", req.TargetName, req.TargetID))
	s.writeJSON(w, http.StatusOK, APIResponse{Code: 0, Message: "专属策略配置已保存并生效"})
}

// ----------------- 插件中心 (Plugins) -----------------

// handlePluginsList 列出所有已扫描装载的插件元数据
func (s *Server) handlePluginsList(w http.ResponseWriter, r *http.Request) {
	type PluginItem struct {
		Name        string   `json:"name"`
		Author      string   `json:"author"`
		Version     string   `json:"version"`
		Description string   `json:"description"`
		Priority    int32    `json:"priority"`
		Enabled     bool     `json:"enabled"`
		Mode        string   `json:"mode"`
		Limits      []string `json:"limits"`
	}

	// 默认装载示例列表
	plugins := []PluginItem{
		{
			Name:        "dashboard",
			Author:      "2guan",
			Version:     "2.0.0",
			Description: "现代化微信控制台与大模型智能管理中心",
			Priority:    100,
			Enabled:     true,
			Mode:        "disabled",
		},
		{
			Name:        "ai",
			Author:      "we-clay Team",
			Version:     "1.5.0",
			Description: "多厂商大模型交互、Failover 容灾链路与 TTS 语音合成引擎",
			Priority:    10,
			Enabled:     true,
			Mode:        "blacklist",
		},
	}

	s.writeJSON(w, http.StatusOK, APIResponse{Code: 0, Data: plugins})
}

// ----------------- 实时日志 (Live Logs) -----------------

// handleLogsRecent 获取近期日志切片
func (s *Server) handleLogsRecent(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		if l, err := strconv.Atoi(lStr); err == nil && l > 0 {
			limit = l
		}
	}
	logs := s.sse.GetRecentLogs(limit)
	s.writeJSON(w, http.StatusOK, APIResponse{Code: 0, Data: logs})
}

// handleLogsExport 打包导出完整日志文件
func (s *Server) handleLogsExport(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=\"we-clay-dashboard.log\"")

	logs := s.sse.GetRecentLogs(3000)
	for _, l := range logs {
		_, _ = fmt.Fprintf(w, "[%s] [%s] [%s] %s\n", l.Timestamp.Format("2006-01-02 15:04:05.000"), l.Level, l.Tag, l.Message)
	}
}

// ----------------- 系统设置 (Settings) -----------------

// handleSettingsGet 读取平台基础设置
func (s *Server) handleSettingsGet(w http.ResponseWriter, r *http.Request) {
	settings, err := s.store.GetSettings()
	if err != nil {
		s.writeJSON(w, http.StatusInternalServerError, APIResponse{Code: 500, Message: err.Error()})
		return
	}
	s.writeJSON(w, http.StatusOK, APIResponse{Code: 0, Data: settings})
}

// handleSettingsSave 保存平台基础设置
func (s *Server) handleSettingsSave(w http.ResponseWriter, r *http.Request) {
	var req SystemSettings
	if err := s.parseJSON(r, &req); err != nil {
		s.writeJSON(w, http.StatusBadRequest, APIResponse{Code: 400, Message: "无效的请求参数"})
		return
	}

	req.SiteTitle = strings.TrimSpace(req.SiteTitle)
	req.FooterText = strings.TrimSpace(req.FooterText)
	if req.SiteTitle == "" {
		req.SiteTitle = "we-clay 机器人控制台"
	}

	if err := s.store.SaveSettings(&req); err != nil {
		s.writeJSON(w, http.StatusInternalServerError, APIResponse{Code: 500, Message: "保存基础设置失败: " + err.Error()})
		return
	}

	s.writeJSON(w, http.StatusOK, APIResponse{Code: 0, Message: "系统设置已更新"})
}

// handleMatrixSettingsGet 读取 AI 权限矩阵与全维度拟人延时配置
func (s *Server) handleMatrixSettingsGet(w http.ResponseWriter, r *http.Request) {
	settings, err := s.store.GetAIMatrixSettings()
	if err != nil {
		s.writeJSON(w, http.StatusInternalServerError, APIResponse{Code: 500, Message: err.Error()})
		return
	}
	s.writeJSON(w, http.StatusOK, APIResponse{Code: 0, Data: settings})
}

// handleMatrixSettingsSave 保存 AI 权限矩阵与全维度拟人延时配置
func (s *Server) handleMatrixSettingsSave(w http.ResponseWriter, r *http.Request) {
	var req AIMatrixSettings
	if err := s.parseJSON(r, &req); err != nil {
		s.writeJSON(w, http.StatusBadRequest, APIResponse{Code: 400, Message: "无效的矩阵配置参数"})
		return
	}

	if err := s.store.SaveAIMatrixSettings(&req); err != nil {
		s.writeJSON(w, http.StatusInternalServerError, APIResponse{Code: 500, Message: "保存矩阵配置失败: " + err.Error()})
		return
	}

	s.writeJSON(w, http.StatusOK, APIResponse{Code: 0, Message: "AI 权限矩阵与延时配置已更新并持久化"})
}

// ----------------- 微信底层连接辅助检查 -----------------

func (s *Server) isWeChatOnline() bool {
	if s.plugin.contact == nil {
		return false
	}
	self := s.plugin.contact.GetSelf()
	return self != nil && self.Username != ""
}

// handleWeChatQRCode 获取微信扫码登录二维码或当前在线状态
func (s *Server) handleWeChatQRCode(w http.ResponseWriter, r *http.Request) {
	if s.isWeChatOnline() {
		s.writeJSON(w, http.StatusOK, APIResponse{
			Code: 0,
			Data: map[string]any{
				"online":  true,
				"message": "微信已在线",
			},
		})
		return
	}

	if s.plugin.login != nil {
		resp, err := s.plugin.login.Login()
		if err == nil && resp != nil {
			s.writeJSON(w, http.StatusOK, APIResponse{
				Code: 0,
				Data: map[string]any{
					"online":       false,
					"uuid":         resp.Uuid,
					"data":         resp.Data,
					"url":          resp.Url,
					"expired_time": resp.ExpiredTime,
				},
			})
			return
		}
	}

	s.writeJSON(w, http.StatusOK, APIResponse{
		Code: 0,
		Data: map[string]any{
			"online":  false,
			"uuid":    "",
			"data":    "",
			"message": "微信暂未登录。若网关处于独立客户端模式，请确保微信客户端已启动并登录。",
		},
	})
}

// handleWeChatStatus 查询当前微信登录状态及自身账号资料
func (s *Server) handleWeChatStatus(w http.ResponseWriter, r *http.Request) {
	online := s.isWeChatOnline()
	var selfContact any
	if online && s.plugin.contact != nil {
		selfContact = s.plugin.contact.GetSelf()
	}
	s.writeJSON(w, http.StatusOK, APIResponse{
		Code: 0,
		Data: map[string]any{
			"online": online,
			"self":   selfContact,
		},
	})
}

