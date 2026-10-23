// 文件名：business_api.go
// 主要功能：提供管理员插件工作台、计划任务、素材库和执行记录 API，不允许浏览器提交好友票据。
// 作者：2guan
package dashboard

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/we-clay/we-clay/plugins/toolbox"
	"image"
	"image/png"
	"net/http"
	"os"
	"strings"
	"time"
)

type businessAsset struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Media string `json:"media"`
}

func (s *Server) businessAssets(account string) ([]businessAsset, error) {
	rows, e := s.store.db.Query("SELECT id,name,media FROM business_assets WHERE account=? ORDER BY name", account)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []businessAsset{}
	for rows.Next() {
		var a businessAsset
		if e = rows.Scan(&a.ID, &a.Name, &a.Media); e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func (s *Server) readBusinessAsset(account, id string) (businessAsset, error) {
	var a businessAsset
	e := s.store.db.QueryRow("SELECT id,name,media FROM business_assets WHERE account=? AND id=?", account, id).Scan(&a.ID, &a.Name, &a.Media)
	if e != nil {
		return a, fmt.Errorf("素材不存在")
	}
	return a, nil
}
func (s *Server) saveBusinessAsset(account, name, media string) (businessAsset, error) {
	a := businessAsset{}
	if strings.TrimSpace(name) == "" || len(name) > 120 {
		return a, fmt.Errorf("请输入素材名称（最多 120 字节）")
	}
	raw, e := base64.StdEncoding.DecodeString(media)
	if e != nil || len(raw) > 8<<20 {
		return a, fmt.Errorf("素材无效或超过 8 MB")
	}
	cfg, _, e := image.DecodeConfig(bytes.NewReader(raw))
	if e != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > 5000 || cfg.Height > 5000 || cfg.Width*cfg.Height > 12_000_000 {
		return a, fmt.Errorf("请上传不超过 1200 万像素的 PNG / JPEG 图片")
	}
	img, _, e := image.Decode(bytes.NewReader(raw))
	if e != nil {
		return a, fmt.Errorf("图片无法读取")
	}
	var buf bytes.Buffer
	if e = png.Encode(&buf, img); e != nil {
		return a, e
	}
	s.businessMu.Lock()
	defer s.businessMu.Unlock()
	list, e := s.businessAssets(account)
	if e != nil {
		return a, e
	}
	if len(list) >= 100 {
		return a, fmt.Errorf("每个账号最多 100 个素材")
	}
	url, e := s.saveMedia(buf.Bytes(), "image/png")
	if e != nil {
		return a, e
	}
	a = businessAsset{randomID(), strings.TrimSpace(name), url}
	_, e = s.store.db.Exec("INSERT INTO business_assets VALUES(?,?,?,?)", a.ID, account, a.Name, a.Media)
	return a, e
}
func (s *Server) businessAPI(w http.ResponseWriter, r *http.Request, u User) {
	if e := requireAdmin(u); e != nil {
		writeError(w, 403, e.Error())
		return
	}
	if r.Method == http.MethodGet {
		st := s.store.snapshot()
		account := st.Account.ID
		jobs, e := s.businessJobs(account)
		if e != nil {
			writeError(w, 500, "任务读取失败")
			return
		}
		assets, e := s.businessAssets(account)
		if e != nil {
			writeError(w, 500, "素材读取失败")
			return
		}
		rows, e := s.store.db.Query("SELECT id,plugin,target,time,status,detail FROM business_runs WHERE account=? ORDER BY time DESC,id DESC LIMIT 100", account)
		if e != nil {
			writeError(w, 500, "记录读取失败")
			return
		}
		runs := []businessRun{}
		for rows.Next() {
			var item businessRun
			if e = rows.Scan(&item.ID, &item.Plugin, &item.Target, &item.Time, &item.Status, &item.Detail); e != nil {
				break
			}
			runs = append(runs, item)
		}
		rows.Close()
		if e != nil {
			writeError(w, 500, "记录读取失败")
			return
		}
		writeJSON(w, 200, map[string]any{"jobs": jobs, "assets": assets, "runs": runs, "catalog": toolbox.Catalog})
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, 405, "请求方法不支持")
		return
	}
	var q struct {
		Operation string          `json:"operation"`
		Request   businessRequest `json:"request"`
		Job       businessJob     `json:"job"`
		ID        string          `json:"id"`
		Revision  int             `json:"revision"`
		Name      string          `json:"name"`
		Media     string          `json:"media"`
	}
	if !decode(w, r, &q) {
		return
	}
	var result any = map[string]bool{"ok": true}
	var err error
	requestState := s.store.snapshot()
	account := requestState.Account.ID
	switch q.Operation {
	case "run":
		if q.Request.Send {
			err = fmt.Errorf("请先预览，再发送确认后的结果")
		} else {
			result, err = s.executeBusinessFor(r.Context(), account, requestState.AccountEpoch, q.Request, "工作台")
		}
	case "send":
		err = s.sendBusinessPreview(r.Context(), q.ID)
	case "simulate-event":
		if !s.cfg.Mock {
			err = fmt.Errorf("系统事件模拟仅用于 Mock")
			break
		}
		if q.Request.Plugin == "auto_accept" {
			err = s.HandleFriend(FriendRequest{ID: randomID(), Applicant: q.Request.Target, Comment: q.Request.Query, V1: "mock-" + randomID(), V2: "mock-" + randomID(), Scene: q.Request.Index})
		} else if q.Request.Plugin == "welcome" {
			if strings.TrimSpace(q.Request.Query) == "" {
				err = fmt.Errorf("请输入模拟新成员名称")
				break
			}
			s.dispatchBusiness(Message{ID: randomID(), Target: q.Request.Target, Content: "“" + q.Request.Query + "”通过扫描二维码加入了群聊", Kind: "system", Source: "wechat", Time: time.Now().Format(time.RFC3339)})
		} else {
			err = fmt.Errorf("不支持此事件模拟")
		}
	case "job-save":
		result, err = s.saveBusinessJob(q.Job)
	case "job-delete":
		err = s.deleteBusinessJob(q.ID, q.Revision)
	case "job-run":
		var jobs []businessJob
		jobs, err = s.businessJobs(account)
		if err == nil {
			found := false
			for _, j := range jobs {
				if j.ID == q.ID {
					found = true
					err = s.runBusinessJob(r.Context(), account, requestState.AccountEpoch, j, "manual:"+randomID())
					break
				}
			}
			if !found {
				err = fmt.Errorf("任务不存在")
			}
		}
	case "asset-save":
		result, err = s.saveBusinessAsset(account, q.Name, q.Media)
	case "asset-delete":
		var a businessAsset
		a, err = s.readBusinessAsset(account, q.ID)
		if err == nil {
			_, err = s.store.db.Exec("DELETE FROM business_assets WHERE id=? AND account=?", q.ID, account) // 原媒体可能被已发送的归档引用，不立即删除文件。
			_ = a
		}
	default:
		err = fmt.Errorf("未知工作台操作")
	}
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	s.notify()
	writeJSON(w, 200, result)
}

// resetBusiness 只由管理员的 Mock 重置入口调用，避免测试遗留定时任务继续执行。
func (s *Server) resetBusiness() error {
	if !s.cfg.Mock {
		return fmt.Errorf("仅允许重置 Mock")
	}
	s.businessMu.Lock()
	defer s.businessMu.Unlock()
	for _, table := range []string{"business_jobs", "business_runs", "business_claims", "business_assets", "business_previews"} {
		if _, e := s.store.db.Exec("DELETE FROM " + table); e != nil {
			return e
		}
	}
	s.playMu.Lock()
	defer s.playMu.Unlock()
	for _, table := range []string{"play_turns", "play_games", "play_memes", "play_daily"} {
		if _, e := s.store.db.Exec("DELETE FROM " + table); e != nil {
			return e
		}
	}
	s.soupMu.Lock()
	defer s.soupMu.Unlock()
	for _, table := range []string{"soup_turns", "soup_games", "soup_puzzles", "soup_played"} {
		if _, e := s.store.db.Exec("DELETE FROM " + table); e != nil {
			return e
		}
	}
	return nil
}

// 预览快照绑定账号、目标和结果，点击发送不会重新抽菜单或调用模型。
func (s *Server) saveBusinessPreview(account string, q businessRequest, r businessResult) (string, error) {
	id := randomID()
	q.Media = "" // 背景图已经生成结果，不重复存储原始大块输入。
	raw, e := json.Marshal(struct {
		Request businessRequest
		Result  businessResult
	}{q, r})
	if e != nil {
		return "", e
	}
	_, e = s.store.db.Exec("INSERT INTO business_previews VALUES(?,?,?,0,?)", id, account, time.Now().Format(time.RFC3339), raw)
	if e == nil {
		_, _ = s.store.db.Exec("DELETE FROM business_previews WHERE time < ?", time.Now().Add(-time.Hour).Format(time.RFC3339))
	}
	if e == nil {
		_, _ = s.store.db.Exec("DELETE FROM business_previews WHERE id IN (SELECT id FROM business_previews ORDER BY time DESC LIMIT -1 OFFSET 200)")
	}
	return id, e
}
func (s *Server) sendBusinessPreview(ctx context.Context, id string) error {
	st := s.store.snapshot()
	account := st.Account.ID
	epoch := st.AccountEpoch
	var raw []byte
	var q struct {
		Request businessRequest
		Result  businessResult
	}
	e := s.store.db.QueryRow("SELECT body FROM business_previews WHERE id=? AND account=? AND used=0 AND time>=?", id, account, time.Now().Add(-time.Hour).Format(time.RFC3339)).Scan(&raw)
	if e != nil {
		return fmt.Errorf("预览已发送或已过期，请重新预览")
	}
	if json.Unmarshal(raw, &q) != nil {
		return fmt.Errorf("预览数据无效")
	}
	if !businessAllowed(s.store.snapshot(), q.Request.Plugin, q.Request.Target) {
		return fmt.Errorf("插件未启用或目标不在生效范围")
	}
	if q.Result.Media != "" && strings.HasPrefix(q.Result.Media, "/media/") {
		q.Result.data, e = os.ReadFile(s.businessMediaPath(q.Result.Media))
		if e != nil {
			return fmt.Errorf("预览媒体不可读取")
		}
	}
	if e = s.loadBusinessImages(&q.Result); e != nil {
		return e
	}
	res, e := s.store.db.Exec("UPDATE business_previews SET used=1 WHERE id=? AND account=? AND used=0", id, account)
	if e != nil {
		return e
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return fmt.Errorf("预览已经发送")
	}
	run, e := s.beginBusiness(account, q.Request, "running")
	if e != nil {
		return e
	}
	e = s.deliverBusinessFor(ctx, account, epoch, q.Request, q.Result)
	if e != nil {
		s.finishBusiness(run, "unknown", "发送未确认，不自动重试："+e.Error())
	} else {
		s.finishBusiness(run, "sent", "已发送工作台预览结果")
	}
	return e
}
