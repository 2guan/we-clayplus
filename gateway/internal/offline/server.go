// Package offline 提供 gateway 的脱机 / Mock 预览模式支持。
// 启动轻量内存级 gRPC core 契约适配器，无需下载或运行独立 core，零微信网络风控。
package offline

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	baseapi "github.com/we-clay/we-clay/gateway/internal/api/base"
	chatroomapi "github.com/we-clay/we-clay/gateway/internal/api/chatroom"
	contactapi "github.com/we-clay/we-clay/gateway/internal/api/contact"
	messageapi "github.com/we-clay/we-clay/gateway/internal/api/message"
	momentsapi "github.com/we-clay/we-clay/gateway/internal/api/moments"
	userapi "github.com/we-clay/we-clay/gateway/internal/api/user"
	"github.com/we-clay/we-clay/sdk/corewire"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
	_ "modernc.org/sqlite"
)

// Server 脱机 / Mock Core 服务实例
type Server struct {
	corewire.UnimplementedCoreServiceServer

	Address       string
	Token         string
	listener      net.Listener
	grpcServer    *grpc.Server
	loginListener net.Listener
	loginHttpSrv  *http.Server

	mu          sync.RWMutex
	dataDir     string
	dbPath      string
	sessionPath string

	selfUsername string
	selfNickname string
	selfUin      uint32
	selfMobile   string
	selfAvatar   string

	contacts  map[string]contactInfo
	chatrooms map[string]chatroomInfo

	eventStreams []grpc.BidiStreamingServer[corewire.EventControl, corewire.CoreEvent]
	msgIDCounter uint64
}

type contactInfo struct {
	Username string
	Nickname string
	Remark   string
	Avatar   string
	Gender   baseapi.Gender
}

type chatroomInfo struct {
	ID      string
	Name    string
	Owner   string
	Members []chatroomMemberInfo
}

type chatroomMemberInfo struct {
	Username    string
	Nickname    string
	DisplayName string
	Avatar      string
}

// Start 启动脱机 Mock Core 服务
func Start(ctx context.Context, dataDir string) (*Server, error) {
	listenAddr := "127.0.0.1:0"
	if addr := os.Getenv("WE_CLAY_MOCK_CORE_ADDRESS"); addr != "" {
		listenAddr = addr
	} else if port := os.Getenv("WE_CLAY_CORE_PORT"); port != "" {
		listenAddr = "127.0.0.1:" + port
	}

	l, err := net.Listen("tcp", listenAddr)
	if err != nil {
		// 端口被占时平滑降级到随机可用端口
		l, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, fmt.Errorf("创建脱机服务监听端口失败: %w", err)
		}
	}

	srv := &Server{
		Address:      l.Addr().String(),
		Token:        "offline-mock-token",
		listener:     l,
		dataDir:      dataDir,
		contacts:     make(map[string]contactInfo),
		chatrooms:    make(map[string]chatroomInfo),
		msgIDCounter: uint64(time.Now().UnixNano()),
	}

	// 1. 初始化用户信息与数据源
	srv.initUserData()

	// 2. 启动 gRPC 服务
	srv.grpcServer = grpc.NewServer(
		grpc.MaxRecvMsgSize(corewire.MaxMessageBytes),
		grpc.MaxSendMsgSize(corewire.MaxMessageBytes),
	)
	corewire.RegisterCoreServiceServer(srv.grpcServer, srv)

	go func() {
		if err := srv.grpcServer.Serve(l); err != nil && err != grpc.ErrServerStopped {
			slog.Debug("[offline core] gRPC 退出", "err", err)
		}
	}()

	// 3. 可选启动登录展示 HTTP 服务（若指定登录地址）
	loginAddr := os.Getenv("WE_CLAY_LOGIN_ADDRESS")
	if loginAddr == "" {
		if p := os.Getenv("WE_CLAY_LOGIN_PORT"); p != "" {
			loginAddr = "127.0.0.1:" + p
		}
	}
	if loginAddr != "" && loginAddr != "-" {
		loginListener, lErr := net.Listen("tcp", loginAddr)
		if lErr == nil {
			srv.loginListener = loginListener
			loginMux := http.NewServeMux()
			loginMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				fmt.Fprintf(w, `<!DOCTYPE html><html><head><meta charset="utf-8"><title>微信脱机扫码登录</title><style>body{font-family:sans-serif;display:flex;flex-direction:column;align-items:center;justify-content:center;height:100vh;margin:0;background:#f5f7fa;color:#333;}.card{background:#fff;padding:32px;border-radius:16px;box-shadow:0 8px 30px rgba(0,0,0,0.08);text-align:center;max-width:380px;}.badge{display:inline-block;padding:4px 12px;background:#e6f7ed;color:#10b981;border-radius:20px;font-size:12px;font-weight:600;margin-bottom:16px;}</style></head><body><div class="card"><div class="badge">脱机 Mock 预览模式</div><h2 style="margin:0 0 8px;">微信已就绪</h2><p style="color:#666;font-size:14px;margin-bottom:20px;">当前为脱机免扫码模式，用户【%s】直接登录成功！<br>请访问 Dashboard 控制台开始管理。</p><a href="http://127.0.0.1:5699" style="display:inline-block;padding:10px 20px;background:#10b981;color:#fff;text-decoration:none;border-radius:8px;font-size:14px;">前往控制台 (5699)</a></div></body></html>`, srv.selfNickname)
			})
			loginHttpSrv := &http.Server{Handler: loginMux}
			srv.loginHttpSrv = loginHttpSrv
			go func() {
				_ = loginHttpSrv.Serve(loginListener)
			}()
			slog.Info("[offline core] 登录引导页面已就绪", "address", loginAddr)
		}
	}

	go func() {
		<-ctx.Done()
		srv.Close()
	}()

	slog.Info("[offline core] 脱机/Mock Core 服务启动成功",
		"address", srv.Address,
		"user", srv.selfNickname,
		"contacts", len(srv.contacts),
		"chatrooms", len(srv.chatrooms))

	return srv, nil
}

// Close 关闭脱机服务
func (s *Server) Close() {
	if s.loginHttpSrv != nil {
		_ = s.loginHttpSrv.Close()
	}
	if s.loginListener != nil {
		_ = s.loginListener.Close()
	}
	if s.grpcServer != nil {
		s.grpcServer.GracefulStop()
	}
	if s.listener != nil {
		_ = s.listener.Close()
	}
}

// generateAvatar 为联系人生成美观、现代的高级渐变 SVG Base64 头像，100% 离线支持
func generateAvatar(name, username string) string {
	gradients := [][2]string{
		{"#4F46E5", "#06B6D4"}, // Indigo -> Cyan
		{"#8B5CF6", "#EC4899"}, // Purple -> Pink
		{"#10B981", "#3B82F6"}, // Emerald -> Blue
		{"#F59E0B", "#EF4444"}, // Amber -> Red
		{"#6366F1", "#8B5CF6"}, // Indigo -> Violet
		{"#14B8A6", "#10B981"}, // Teal -> Emerald
		{"#EC4899", "#F43F5E"}, // Pink -> Rose
		{"#3B82F6", "#1D4ED8"}, // Blue -> Dark Blue
		{"#84CC16", "#10B981"}, // Lime -> Emerald
		{"#F97316", "#F59E0B"}, // Orange -> Amber
	}

	hash := 0
	for _, c := range username {
		hash = (hash*31 + int(c)) & 0x7fffffff
	}
	grad := gradients[hash%len(gradients)]

	runes := []rune(strings.TrimSpace(name))
	displayChar := "微"
	if len(runes) > 0 {
		displayChar = string(runes[len(runes)-1])
	}

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100" viewBox="0 0 100 100"><defs><linearGradient id="g" x1="0%%" y1="0%%" x2="100%%" y2="100%%"><stop offset="0%%" stop-color="%s"/><stop offset="100%%" stop-color="%s"/></linearGradient></defs><rect width="100" height="100" rx="20" fill="url(#g)"/><text x="50" y="55" font-family="-apple-system,BlinkMacSystemFont,PingFang SC,Segoe UI,sans-serif" font-size="44" font-weight="600" fill="#ffffff" text-anchor="middle" dominant-baseline="middle">%s</text></svg>`,
		grad[0], grad[1], html.EscapeString(displayChar))

	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(svg))
}

// initUserData 从本地 session.json 与 SQLite 恢复真实缓存，并补充丰富 Mock 数据
func (s *Server) initUserData() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 默认账号数据
	s.selfUsername = "wxid_lft3oxct08l622"
	s.selfNickname = "肉丸"
	s.selfUin = 3838879310
	s.selfMobile = "15611256615"
	s.selfAvatar = generateAvatar("肉丸", s.selfUsername)

	// 尝试寻找 session.json 读取真实登录数据
	possibleSessions := []string{
		filepath.Join(s.dataDir, "..", "..", "runtime", "core", "session.json"),
		filepath.Join("runtime", "core", "session.json"),
		filepath.Join(s.dataDir, "session.json"),
	}
	for _, p := range possibleSessions {
		if raw, err := os.ReadFile(p); err == nil {
			var sess struct {
				User struct {
					Uin      uint32 `json:"uin"`
					Username string `json:"username"`
					Nickname string `json:"nickname"`
					Avatar   string `json:"avatar"`
					Mobile   string `json:"mobile"`
				} `json:"user"`
			}
			if json.Unmarshal(raw, &sess) == nil && sess.User.Username != "" {
				s.selfUsername = sess.User.Username
				if sess.User.Nickname != "" {
					s.selfNickname = sess.User.Nickname
				}
				if sess.User.Uin != 0 {
					s.selfUin = sess.User.Uin
				}
				if sess.User.Mobile != "" {
					s.selfMobile = sess.User.Mobile
				}
				if sess.User.Avatar != "" && (strings.HasPrefix(sess.User.Avatar, "http") || strings.HasPrefix(sess.User.Avatar, "data:")) {
					s.selfAvatar = sess.User.Avatar
				} else {
					s.selfAvatar = generateAvatar(s.selfNickname, s.selfUsername)
				}
				s.sessionPath = p
				break
			}
		}
	}

	// 尝试寻找 SQLite dashboard.db 读取真实通讯录与会话记录
	possibleDBs := []string{
		filepath.Join(s.dataDir, "dashboard.db", "dashboard.db"),
		filepath.Join(s.dataDir, "dashboard.db"),
		filepath.Join("runtime", "gateway", "data", "dashboard.db", "dashboard.db"),
		filepath.Join("runtime", "gateway", "data", "dashboard.db"),
	}
	for _, p := range possibleDBs {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			s.dbPath = p
			s.loadFromSQLite(p)
			break
		}
	}

	// 确保基础通讯录与群聊充足完整
	s.ensureMockContactsAndChatrooms()
}

func (s *Server) loadFromSQLite(dbPath string) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return
	}
	defer db.Close()

	// 1. 读取已知群聊及其名称
	roomRows, err := db.Query(`
		SELECT session_id, sender_name 
		FROM chat_audit_messages 
		WHERE sender_id = session_id AND session_id LIKE '%@chatroom'
	`)
	if err == nil {
		for roomRows.Next() {
			var rid, rname string
			if err := roomRows.Scan(&rid, &rname); err == nil && rid != "" {
				if rname == "" {
					rname = "微信群聊 (" + rid[:8] + ")"
				}
				s.chatrooms[rid] = chatroomInfo{
					ID:    rid,
					Name:  rname,
					Owner: s.selfUsername,
				}
			}
		}
		roomRows.Close()
	}

	// 2. 检查是否有未具名的群聊
	allRoomRows, err := db.Query(`
		SELECT DISTINCT session_id 
		FROM chat_audit_messages 
		WHERE session_id LIKE '%@chatroom'
	`)
	if err == nil {
		for allRoomRows.Next() {
			var rid string
			if err := allRoomRows.Scan(&rid); err == nil && rid != "" {
				if _, ok := s.chatrooms[rid]; !ok {
					s.chatrooms[rid] = chatroomInfo{
						ID:    rid,
						Name:  "微信群聊 (" + rid[:8] + ")",
						Owner: s.selfUsername,
					}
				}
			}
		}
		allRoomRows.Close()
	}

	// 3. 为每个群聊加载历史发言成员并注入头像
	for rid, rinfo := range s.chatrooms {
		memRows, err := db.Query(`
			SELECT DISTINCT sender_id, sender_name 
			FROM chat_audit_messages 
			WHERE session_id = ? AND sender_id NOT LIKE '%@chatroom' AND sender_id != ''
		`, rid)
		if err == nil {
			members := []chatroomMemberInfo{
				{
					Username:    s.selfUsername,
					Nickname:    s.selfNickname,
					DisplayName: s.selfNickname,
					Avatar:      s.selfAvatar,
				},
			}
			seenMem := map[string]bool{s.selfUsername: true}
			for memRows.Next() {
				var mid, mname string
				if err := memRows.Scan(&mid, &mname); err == nil && mid != "" && !seenMem[mid] {
					seenMem[mid] = true
					if mname == "" {
						mname = mid
					}
					mAvatar := generateAvatar(mname, mid)
					members = append(members, chatroomMemberInfo{
						Username:    mid,
						Nickname:    mname,
						DisplayName: mname,
						Avatar:      mAvatar,
					})
					// 群成员同时作为通讯录联系人
					if mid != s.selfUsername {
						if _, ok := s.contacts[mid]; !ok {
							s.contacts[mid] = contactInfo{
								Username: mid,
								Nickname: mname,
								Avatar:   mAvatar,
								Gender:   baseapi.Gender_GENDER_MALE,
							}
						}
					}
				}
			}
			memRows.Close()
			rinfo.Members = members
			s.chatrooms[rid] = rinfo
		}
	}

	// 4. 读取 1 对 1 会话中的全部联系人
	directRows, err := db.Query(`
		SELECT DISTINCT session_id, sender_name 
		FROM chat_audit_messages 
		WHERE session_id NOT LIKE '%@chatroom' AND session_id != 'weixin' AND session_id != ''
	`)
	if err == nil {
		for directRows.Next() {
			var sid, sname string
			if err := directRows.Scan(&sid, &sname); err == nil && sid != "" && sid != s.selfUsername {
				if sname == "" {
					sname = sid
				}
				if c, exists := s.contacts[sid]; exists {
					if c.Nickname == sid && sname != sid {
						c.Nickname = sname
						c.Avatar = generateAvatar(sname, sid)
						s.contacts[sid] = c
					}
				} else {
					s.contacts[sid] = contactInfo{
						Username: sid,
						Nickname: sname,
						Avatar:   generateAvatar(sname, sid),
						Gender:   baseapi.Gender_GENDER_MALE,
					}
				}
			}
		}
		directRows.Close()
	}
}

func (s *Server) ensureMockContactsAndChatrooms() {
	defaults := []contactInfo{
		{Username: "doubleguan", Nickname: "管管", Remark: "双管", Gender: baseapi.Gender_GENDER_MALE},
		{Username: "HIT1100300818", Nickname: "风在林梢鸟在叫", Gender: baseapi.Gender_GENDER_MALE},
		{Username: "wxid_9183591837612", Nickname: "万仔（陈鑫涛Xintao）", Gender: baseapi.Gender_GENDER_MALE},
		{Username: "wxid_r7crjqt7btbs21", Nickname: "胖猫", Gender: baseapi.Gender_GENDER_FEMALE},
		{Username: "wxid_v0g1rzpcm0sb22", Nickname: "流体熊🐻", Gender: baseapi.Gender_GENDER_MALE},
		{Username: "wxid_a35k4e8c4swr21", Nickname: "羽秦", Gender: baseapi.Gender_GENDER_FEMALE},
		{Username: "wxid_5tsrilwp5uvi22", Nickname: "流霜", Gender: baseapi.Gender_GENDER_FEMALE},
		{Username: "wxid_eqks3o332gek12", Nickname: "冯献| Kelton（时差-15H有事留言）", Gender: baseapi.Gender_GENDER_MALE},
		{Username: "wxid_q70q1jimyb7c12", Nickname: "Marsrun", Gender: baseapi.Gender_GENDER_MALE},
		{Username: "wxid_vhdded63atkj22", Nickname: "柒喜", Gender: baseapi.Gender_GENDER_FEMALE},
		{Username: "tanghaowen12311", Nickname: "Stefan", Gender: baseapi.Gender_GENDER_MALE},
		{Username: "wanzhi440802", Nickname: "丸纸", Gender: baseapi.Gender_GENDER_FEMALE},
		{Username: "KumaDou", Nickname: "Focus®", Gender: baseapi.Gender_GENDER_MALE},
		{Username: "wxid_fi9xmxjwgszt19", Nickname: "有福的分身", Gender: baseapi.Gender_GENDER_MALE},
		{Username: "wxid_wzuszkze3vvs22", Nickname: "撒欢儿的丁丁", Gender: baseapi.Gender_GENDER_MALE},
	}
	for _, c := range defaults {
		if cur, ok := s.contacts[c.Username]; !ok {
			c.Avatar = generateAvatar(c.Nickname, c.Username)
			s.contacts[c.Username] = c
		} else if cur.Avatar == "" {
			cur.Avatar = generateAvatar(cur.Nickname, cur.Username)
			s.contacts[c.Username] = cur
		}
	}

	room1 := "51872154794@chatroom"
	if r, ok := s.chatrooms[room1]; !ok || len(r.Members) <= 1 {
		s.chatrooms[room1] = chatroomInfo{
			ID:    room1,
			Name:  "Chat～鸡屁踢",
			Owner: s.selfUsername,
			Members: []chatroomMemberInfo{
				{Username: s.selfUsername, Nickname: s.selfNickname, DisplayName: s.selfNickname, Avatar: s.selfAvatar},
				{Username: "doubleguan", Nickname: "管管", DisplayName: "双管", Avatar: generateAvatar("管管", "doubleguan")},
				{Username: "HIT1100300818", Nickname: "风在林梢鸟在叫", DisplayName: "风林", Avatar: generateAvatar("风在林梢鸟在叫", "HIT1100300818")},
				{Username: "wxid_9183591837612", Nickname: "万仔（陈鑫涛Xintao）", DisplayName: "万仔", Avatar: generateAvatar("万仔", "wxid_9183591837612")},
				{Username: "wxid_r7crjqt7btbs21", Nickname: "胖猫", DisplayName: "胖猫", Avatar: generateAvatar("胖猫", "wxid_r7crjqt7btbs21")},
			},
		}
	}

	room2 := "56844300491@chatroom"
	if r, ok := s.chatrooms[room2]; !ok || len(r.Members) <= 1 {
		s.chatrooms[room2] = chatroomInfo{
			ID:    room2,
			Name:  "下半年必须暴富",
			Owner: s.selfUsername,
			Members: []chatroomMemberInfo{
				{Username: s.selfUsername, Nickname: s.selfNickname, DisplayName: s.selfNickname, Avatar: s.selfAvatar},
				{Username: "The_Life_style", Nickname: "有福", DisplayName: "有福", Avatar: generateAvatar("有福", "The_Life_style")},
				{Username: "doubleguan", Nickname: "管管", DisplayName: "双管", Avatar: generateAvatar("管管", "doubleguan")},
				{Username: "vagaova", Nickname: "强", DisplayName: "强哥", Avatar: generateAvatar("强", "vagaova")},
			},
		}
	}
}

// Events 实现 corewire.CoreServiceServer 事件长流
func (s *Server) Events(stream grpc.BidiStreamingServer[corewire.EventControl, corewire.CoreEvent]) error {
	control, err := stream.Recv()
	if err != nil {
		return err
	}
	_ = control

	// 告知客户端就绪
	if err := stream.Send(&corewire.CoreEvent{Ready: true}); err != nil {
		return err
	}

	s.mu.Lock()
	s.eventStreams = append(s.eventStreams, stream)
	s.mu.Unlock()

	<-stream.Context().Done()
	return stream.Context().Err()
}

// Invoke 实现 corewire.CoreServiceServer REST/RPC 调用
func (s *Server) Invoke(ctx context.Context, req *corewire.CallRequest) (*corewire.CallResponse, error) {
	path := req.GetPath()
	method := req.GetMethod()

	switch {
	case path == "/api/user/profile" && method == "GET":
		return s.handleUserProfile()

	case path == "/api/contacts" && method == "GET":
		return s.handleContactsList()

	case path == "/api/contacts/detail" && method == "POST":
		return s.handleContactsDetail(req.GetBody())

	case strings.HasPrefix(path, "/api/chatroom/info/"):
		roomID := strings.TrimPrefix(path, "/api/chatroom/info/")
		roomID, _ = url.PathUnescape(roomID)
		return s.handleChatroomInfo(roomID)

	case strings.HasPrefix(path, "/api/chatroom/members/"):
		roomID := strings.TrimPrefix(path, "/api/chatroom/members/")
		roomID, _ = url.PathUnescape(roomID)
		return s.handleChatroomMembers(roomID)

	case path == "/api/moments/timeline" && method == "GET":
		return s.handleMomentsTimeline()

	case strings.HasPrefix(path, "/api/moments/user/") && method == "GET":
		username := strings.TrimPrefix(path, "/api/moments/user/")
		username, _ = url.PathUnescape(username)
		return s.handleMomentsUser(username)

	case path == "/api/message/text" && method == "POST":
		return s.handleMessageText(req.GetBody())

	case strings.HasPrefix(path, "/api/moments/like/"):
		return s.handleSuccessEnvelope()

	case strings.HasPrefix(path, "/api/moments/comment/"):
		return s.handleSuccessEnvelope()

	case path == "/api/message/revoke" && method == "POST":
		return s.handleSuccessEnvelope()

	default:
		return s.handleSuccessEnvelope()
	}
}

func (s *Server) handleSuccessEnvelope() (*corewire.CallResponse, error) {
	envelope := map[string]any{
		"code": 0,
		"data": map[string]any{
			"base_response": map[string]any{"code": 0},
		},
		"message": "",
	}
	body, _ := json.Marshal(envelope)
	return &corewire.CallResponse{Status: 200, ContentType: "application/json", Body: body}, nil
}

func (s *Server) handleUserProfile() (*corewire.CallResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	resp := &userapi.GetProfileResponse{
		BaseResponse: &baseapi.BaseResponse{Code: proto.Int32(0)},
		UserInfo: &messageapi.ModifyUserInfo{
			UserName: wrapperspb.String(s.selfUsername),
			NickName: wrapperspb.String(s.selfNickname),
			Uin:      proto.Uint32(s.selfUin),
			Mobile:   wrapperspb.String(s.selfMobile),
		},
		UserInfoExt: &messageapi.UserInfoExtend{
			SmallAvatarUrl: proto.String(s.selfAvatar),
			BigAvatarUrl:   proto.String(s.selfAvatar),
		},
	}

	dataBytes, err := protojson.Marshal(resp)
	if err != nil {
		return nil, err
	}

	envelope := map[string]any{
		"code":    0,
		"data":    json.RawMessage(dataBytes),
		"message": "",
	}
	body, _ := json.Marshal(envelope)
	return &corewire.CallResponse{Status: 200, ContentType: "application/json", Body: body}, nil
}

func (s *Server) handleContactsList() (*corewire.CallResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var list []string
	for sid := range s.chatrooms {
		list = append(list, sid)
	}
	for uid := range s.contacts {
		list = append(list, uid)
	}

	envelope := map[string]any{
		"code":    0,
		"data":    list,
		"message": "",
	}
	body, _ := json.Marshal(envelope)
	return &corewire.CallResponse{Status: 200, ContentType: "application/json", Body: body}, nil
}

func (s *Server) handleContactsDetail(reqBody []byte) (*corewire.CallResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var usernames []string
	_ = json.Unmarshal(reqBody, &usernames)

	var contactList []*contactapi.ModifyContact
	for _, u := range usernames {
		if room, ok := s.chatrooms[u]; ok {
			var membersList []*contactapi.ModifyContact_ChatroomMemberData_MemberInfo
			for _, m := range room.Members {
				membersList = append(membersList, &contactapi.ModifyContact_ChatroomMemberData_MemberInfo{
					Username:       proto.String(m.Username),
					Nickname:       proto.String(m.Nickname),
					DisplayName:    proto.String(m.DisplayName),
					SmallAvatarUrl: proto.String(m.Avatar),
					BigAvatarUrl:   proto.String(m.Avatar),
				})
			}

			roomAvatar := generateAvatar(room.Name, room.ID)
			contactList = append(contactList, &contactapi.ModifyContact{
				Username:      wrapperspb.String(room.ID),
				Nickname:      wrapperspb.String(room.Name),
				Remark:        wrapperspb.String(""),
				ChatroomOwner: proto.String(room.Owner),
				BigAvatarUrl:  proto.String(roomAvatar),
				Members: &contactapi.ModifyContact_ChatroomMemberData{
					List: membersList,
				},
			})
		} else if c, ok := s.contacts[u]; ok {
			avatar := c.Avatar
			if avatar == "" {
				avatar = generateAvatar(c.Nickname, c.Username)
			}
			contactList = append(contactList, &contactapi.ModifyContact{
				Username:     wrapperspb.String(c.Username),
				Nickname:     wrapperspb.String(c.Nickname),
				Remark:       wrapperspb.String(c.Remark),
				Gender:       c.Gender.Enum(),
				Country:      proto.String("CN"),
				Province:     proto.String("Guangdong"),
				City:         proto.String("Shenzhen"),
				BigAvatarUrl: proto.String(avatar),
			})
		} else {
			contactList = append(contactList, &contactapi.ModifyContact{
				Username:     wrapperspb.String(u),
				Nickname:     wrapperspb.String(u),
				Remark:       wrapperspb.String(""),
				BigAvatarUrl: proto.String(generateAvatar(u, u)),
			})
		}
	}

	resp := &contactapi.GetContactDetailResponse{
		BaseResponse: &baseapi.BaseResponse{Code: proto.Int32(0)},
		ContactCount: proto.Int32(int32(len(contactList))),
		ContactList:  contactList,
	}

	dataBytes, err := protojson.Marshal(resp)
	if err != nil {
		return nil, err
	}

	envelope := map[string]any{
		"code":    0,
		"data":    json.RawMessage(dataBytes),
		"message": "",
	}
	body, _ := json.Marshal(envelope)
	return &corewire.CallResponse{Status: 200, ContentType: "application/json", Body: body}, nil
}

func (s *Server) handleChatroomInfo(roomID string) (*corewire.CallResponse, error) {
	s.mu.RLock()
	room, ok := s.chatrooms[roomID]
	s.mu.RUnlock()

	announcement := "欢迎加入群聊！大家畅所欲言，友好交流。"
	if ok {
		announcement = fmt.Sprintf("【%s】群公告：欢迎入群，请遵守群规！", room.Name)
	}

	resp := &chatroomapi.GetChatroomInfoDetailResponse{
		BaseResponse: &baseapi.BaseResponse{Code: proto.Int32(0)},
		Announcement: proto.String(announcement),
	}

	dataBytes, _ := protojson.Marshal(resp)
	envelope := map[string]any{
		"code":    0,
		"data":    json.RawMessage(dataBytes),
		"message": "",
	}
	body, _ := json.Marshal(envelope)
	return &corewire.CallResponse{Status: 200, ContentType: "application/json", Body: body}, nil
}

func (s *Server) handleChatroomMembers(roomID string) (*corewire.CallResponse, error) {
	s.mu.RLock()
	room, ok := s.chatrooms[roomID]
	s.mu.RUnlock()

	var list []*chatroomapi.ListMembersResponse_Result_MemberInfo
	if ok {
		for _, m := range room.Members {
			list = append(list, &chatroomapi.ListMembersResponse_Result_MemberInfo{
				Username:       proto.String(m.Username),
				Nickname:       proto.String(m.Nickname),
				DisplayName:    proto.String(m.DisplayName),
				BigAvatarUrl:   proto.String(m.Avatar),
				SmallAvatarUrl: proto.String(m.Avatar),
			})
		}
	}

	resp := &chatroomapi.ListMembersResponse{
		BaseResponse: &baseapi.BaseResponse{Code: proto.Int32(0)},
		Chatroom:     proto.String(roomID),
		Result: &chatroomapi.ListMembersResponse_Result{
			Count: proto.Uint32(uint32(len(list))),
			List:  list,
		},
	}

	dataBytes, _ := protojson.Marshal(resp)
	envelope := map[string]any{
		"code":    0,
		"data":    json.RawMessage(dataBytes),
		"message": "",
	}
	body, _ := json.Marshal(envelope)
	return &corewire.CallResponse{Status: 200, ContentType: "application/json", Body: body}, nil
}

func (s *Server) getMockMoments(selfUser, selfNick string) []*messageapi.MomentsObject {
	now := uint32(time.Now().Unix())

	xml1 := `<TimelineObject>
		<contentObject>
			<title>今天天气真不错，开启 we-clayplus 脱机 Mock 模式测试控制台 🚀 界面质感非常棒！</title>
			<mediaList>
				<media>
					<url>https://images.unsplash.com/photo-1579783900882-c0d3dad7b119?w=600</url>
					<thumb>https://images.unsplash.com/photo-1579783900882-c0d3dad7b119?w=300</thumb>
				</media>
			</mediaList>
		</contentObject>
	</TimelineObject>`

	xml2 := `<TimelineObject>
		<contentObject>
			<title>分享周末随手拍的照片 📸 阳光正好，微风不燥。</title>
			<mediaList>
				<media>
					<url>https://images.unsplash.com/photo-1507525428034-b723cf961d3e?w=600</url>
					<thumb>https://images.unsplash.com/photo-1507525428034-b723cf961d3e?w=300</thumb>
				</media>
				<media>
					<url>https://images.unsplash.com/photo-1519046904884-53103b34b206?w=600</url>
					<thumb>https://images.unsplash.com/photo-1519046904884-53103b34b206?w=300</thumb>
				</media>
				<media>
					<url>https://images.unsplash.com/photo-1473496169904-658ba7c44d8a?w=600</url>
					<thumb>https://images.unsplash.com/photo-1473496169904-658ba7c44d8a?w=300</thumb>
				</media>
				<media>
					<url>https://images.unsplash.com/photo-1498050108023-c5249f4df085?w=600</url>
					<thumb>https://images.unsplash.com/photo-1498050108023-c5249f4df085?w=300</thumb>
				</media>
			</mediaList>
		</contentObject>
	</TimelineObject>`

	xml3 := `<TimelineObject>
		<contentObject>
			<title>we-clayplus 现代化管理控制台脱机运行验证完毕：零风控、免扫码、数据无损预览！🎉</title>
			<mediaList></mediaList>
		</contentObject>
	</TimelineObject>`

	return []*messageapi.MomentsObject{
		{
			Id:                proto.Uint64(10001),
			Username:          proto.String(selfUser),
			Nickname:          proto.String(selfNick),
			CreateTime:        proto.Uint32(now - 1800),
			ObjectDescription: wrapperspb.String(xml1),
			LikeCount:         proto.Uint32(2),
			LikeUserList: []*messageapi.MomentsObject_CommentInfo{
				{Username: proto.String("doubleguan"), Nickname: proto.String("管管")},
				{Username: proto.String("HIT1100300818"), Nickname: proto.String("风在林梢鸟在叫")},
			},
			CommentCount: proto.Uint32(2),
			CommentUserList: []*messageapi.MomentsObject_CommentInfo{
				{CommentId: proto.Int32(1), Username: proto.String("doubleguan"), Nickname: proto.String("管管"), Content: proto.String("界面响应太丝滑了！")},
				{CommentId: proto.Int32(2), Username: proto.String(selfUser), Nickname: proto.String(selfNick), Content: proto.String("脱机模式非常方便安全 👍")},
			},
		},
		{
			Id:                proto.Uint64(10002),
			Username:          proto.String("wxid_9183591837612"),
			Nickname:          proto.String("万仔（陈鑫涛Xintao）"),
			CreateTime:        proto.Uint32(now - 7200),
			ObjectDescription: wrapperspb.String(xml2),
			LikeCount:         proto.Uint32(3),
			LikeUserList: []*messageapi.MomentsObject_CommentInfo{
				{Username: proto.String(selfUser), Nickname: proto.String(selfNick)},
				{Username: proto.String("wxid_r7crjqt7btbs21"), Nickname: proto.String("胖猫")},
			},
			CommentCount: proto.Uint32(1),
			CommentUserList: []*messageapi.MomentsObject_CommentInfo{
				{CommentId: proto.Int32(3), Username: proto.String(selfUser), Nickname: proto.String(selfNick), Content: proto.String("九宫格相册排版效果很赞！")},
			},
		},
		{
			Id:                proto.Uint64(10003),
			Username:          proto.String("wxid_r7crjqt7btbs21"),
			Nickname:          proto.String("胖猫"),
			CreateTime:        proto.Uint32(now - 86400),
			ObjectDescription: wrapperspb.String(xml3),
			LikeCount:         proto.Uint32(5),
			LikeUserList: []*messageapi.MomentsObject_CommentInfo{
				{Username: proto.String("doubleguan"), Nickname: proto.String("管管")},
				{Username: proto.String(selfUser), Nickname: proto.String(selfNick)},
			},
			CommentCount: proto.Uint32(0),
		},
	}
}

func (s *Server) handleMomentsTimeline() (*corewire.CallResponse, error) {
	s.mu.RLock()
	selfNick := s.selfNickname
	selfUser := s.selfUsername
	s.mu.RUnlock()

	objects := s.getMockMoments(selfUser, selfNick)
	resp := &momentsapi.TimelineResponse{
		BaseResponse: &baseapi.BaseResponse{Code: proto.Int32(0)},
		ObjectCount:  proto.Uint32(uint32(len(objects))),
		ObjectList:   objects,
	}

	dataBytes, _ := protojson.Marshal(resp)
	envelope := map[string]any{
		"code":    0,
		"data":    json.RawMessage(dataBytes),
		"message": "",
	}
	body, _ := json.Marshal(envelope)
	return &corewire.CallResponse{Status: 200, ContentType: "application/json", Body: body}, nil
}

func (s *Server) handleMomentsUser(username string) (*corewire.CallResponse, error) {
	s.mu.RLock()
	selfNick := s.selfNickname
	selfUser := s.selfUsername
	contactName := username
	if c, ok := s.contacts[username]; ok {
		contactName = c.Nickname
	}
	s.mu.RUnlock()

	all := s.getMockMoments(selfUser, selfNick)
	var filtered []*messageapi.MomentsObject
	if username == "" || username == "all" {
		filtered = all
	} else {
		for _, o := range all {
			if o.GetUsername() == username {
				filtered = append(filtered, o)
			}
		}
		// 如果该联系人暂无动态，生成一条该联系人的专属动态
		if len(filtered) == 0 {
			now := uint32(time.Now().Unix())
			xml := fmt.Sprintf(`<TimelineObject>
				<contentObject>
					<title>生活明朗，万物可爱 ✨ 与大家共同探索 AI 智能体技术。</title>
					<mediaList></mediaList>
				</contentObject>
			</TimelineObject>`)
			filtered = append(filtered, &messageapi.MomentsObject{
				Id:                proto.Uint64(uint64(now)),
				Username:          proto.String(username),
				Nickname:          proto.String(contactName),
				CreateTime:        proto.Uint32(now - 3600),
				ObjectDescription: wrapperspb.String(xml),
				LikeCount:         proto.Uint32(1),
				LikeUserList: []*messageapi.MomentsObject_CommentInfo{
					{Username: proto.String(selfUser), Nickname: proto.String(selfNick)},
				},
			})
		}
	}

	resp := &momentsapi.UserPageResponse{
		BaseResponse:     &baseapi.BaseResponse{Code: proto.Int32(0)},
		ObjectCount:      proto.Uint32(uint32(len(filtered))),
		ObjectTotalCount: proto.Uint32(uint32(len(filtered))),
		ObjectList:       filtered,
	}

	dataBytes, _ := protojson.Marshal(resp)
	envelope := map[string]any{
		"code":    0,
		"data":    json.RawMessage(dataBytes),
		"message": "",
	}
	body, _ := json.Marshal(envelope)
	return &corewire.CallResponse{Status: 200, ContentType: "application/json", Body: body}, nil
}

func (s *Server) handleMessageText(reqBody []byte) (*corewire.CallResponse, error) {
	var req struct {
		Receiver string `json:"receiver"`
		Content  string `json:"content"`
		Remind   string `json:"remind"`
	}
	_ = json.Unmarshal(reqBody, &req)

	s.mu.Lock()
	s.msgIDCounter++
	newID := s.msgIDCounter
	clientID := uint64(time.Now().Unix())
	selfUser := s.selfUsername
	selfNick := s.selfNickname
	dbPath := s.dbPath
	s.mu.Unlock()

	// 若存在 SQLite，将出站消息直接归档写入，实现本地消息流立即可视
	if dbPath != "" && req.Content != "" {
		if db, err := sql.Open("sqlite", dbPath); err == nil {
			msgID := strconv.FormatUint(newID, 10)
			_, _ = db.Exec(`
				INSERT OR IGNORE INTO chat_audit_messages 
				(msg_id, session_id, sender_id, sender_name, msg_type, content, is_self, created_at)
				VALUES (?, ?, ?, ?, '文本', ?, 1, CURRENT_TIMESTAMP)
			`, msgID, req.Receiver, selfUser, selfNick, req.Content)
			_ = db.Close()
		}
	}

	resp := &messageapi.SendMessageResponse{
		BaseResponse: &baseapi.BaseResponse{Code: proto.Int32(0)},
		List: []*messageapi.SendMessageResponse_Result{
			{
				Code:       proto.Int64(0),
				NewId:      proto.Uint64(newID),
				ClientId:   proto.Uint64(clientID),
				CreateTime: proto.Uint32(uint32(time.Now().Unix())),
			},
		},
	}

	dataBytes, _ := protojson.Marshal(resp)
	envelope := map[string]any{
		"code":    0,
		"data":    json.RawMessage(dataBytes),
		"message": "",
	}
	body, _ := json.Marshal(envelope)
	return &corewire.CallResponse{Status: 200, ContentType: "application/json", Body: body}, nil
}
