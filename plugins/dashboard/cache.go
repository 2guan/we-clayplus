// 主要功能：管理员按类别清理可再生成的缓存，保留账号、归档、报告与配置素材。
package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type cacheItem struct {
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Count       int64  `json:"count"`
	Bytes       int64  `json:"bytes"`
}
type cacheFile struct {
	name  string
	bytes int64
}

func mediaName(value string) string {
	if !strings.HasPrefix(value, "/media/") {
		return ""
	}
	name := strings.TrimPrefix(value, "/media/")
	if name == "" || filepath.Base(name) != name || strings.Contains(name, "..") {
		return ""
	}
	return name
}

func (s *Server) cacheFiles(kind string, days int, st State) ([]cacheFile, error) {
	dir := s.cfg.DataDir
	if kind == "media" {
		dir = filepath.Join(dir, "media")
	}
	info, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("缓存目录不是本机普通目录")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	protected := map[string]bool{}
	var references strings.Builder
	if kind == "media" {
		for _, m := range st.Messages {
			if m.Self {
				protected[mediaName(m.Media)] = true
			}
		}
		data, _ := json.Marshal(struct {
			Tasks     []Task
			Settings  map[string]any
			Overrides map[string]map[string]any
			History   []Change
		}{st.Tasks, st.Settings, st.Overrides, st.History})
		references.Write(data)
		// 非活动账号和插件素材共用媒体目录，任何引用都必须保护。
		for _, q := range []struct {
			sql  string
			args []any
		}{
			{"SELECT body FROM state_rows WHERE account<>?", []any{st.Account.ID}},
			{"SELECT body FROM documents WHERE id LIKE 'account:%'", nil},
			{"SELECT local FROM stickers WHERE local<>''", nil},
			{"SELECT media FROM business_assets WHERE media<>''", nil},
			{"SELECT body FROM business_previews WHERE used=0 AND time>=?", []any{time.Now().Add(-time.Hour).Format(time.RFC3339)}},
		} {
			rows, e := s.store.db.Query(q.sql, q.args...)
			if e != nil {
				return nil, e
			}
			for rows.Next() {
				var raw string
				if e = rows.Scan(&raw); e != nil {
					rows.Close()
					return nil, e
				}
				references.WriteString(raw)
				references.WriteByte('\n')
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return nil, e
			}
		}
	}
	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	if days == 0 {
		cutoff = time.Now().Add(-10 * time.Minute)
	}
	if kind == "temporary" && cutoff.After(time.Now().Add(-24*time.Hour)) {
		cutoff = time.Now().Add(-24 * time.Hour)
	}
	files := []cacheFile{}
	refText := references.String()
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
			continue
		}
		if kind == "temporary" && !(strings.HasPrefix(entry.Name(), ".download-") && strings.HasSuffix(entry.Name(), ".part")) {
			continue
		}
		fi, e := entry.Info()
		if e != nil {
			return nil, e
		}
		if !fi.Mode().IsRegular() || !fi.ModTime().Before(cutoff) {
			continue
		}
		if kind == "media" && (protected[entry.Name()] || strings.Contains(refText, "/media/"+entry.Name())) {
			continue
		}
		files = append(files, cacheFile{entry.Name(), fi.Size()})
	}
	return files, nil
}

func (s *Server) cacheStats(days int) ([]cacheItem, error) {
	st := s.store.snapshot()
	items := []cacheItem{
		{Kind: "logs", Name: "控制台日志", Description: "包括 DEBUG 请求、响应原文和操作日志；清理当前微信账号的记录，不删除聊天归档。"},
		{Kind: "avatars", Name: "头像缓存", Description: "头像由浏览器缓存，服务端不落盘。刷新后各页面重新获取头像，浏览器旧副本按自身策略过期。"},
		{Kind: "media", Name: "文件与媒体缓存", Description: "清理当前账号已下载的入站附件及无引用文件。保护已发送素材、音色样本、垫图、表情和其他账号引用；跳过最近 10 分钟的新文件。过期微信资源可能无法再次下载。"},
		{Kind: "insights", Name: "洞察复用缓存", Description: "清理本机所有账号的朋友圈采集与图片处理缓存，后续分析重新采集；保留报告原文、追问和固定资料快照。此项不按天数筛选。"},
		{Kind: "temporary", Name: "下载临时文件", Description: "清理未完成下载遗留的临时文件，始终跳过最近 24 小时的文件。"},
	}
	where := "account=?"
	args := []any{st.Account.ID}
	if days > 0 {
		where += " AND julianday(time)<julianday(?)"
		args = append(args, time.Now().Add(-time.Duration(days)*24*time.Hour).Format(time.RFC3339))
	}
	if err := s.store.db.QueryRow("SELECT count(*),coalesce(sum(length(CAST(message AS BLOB))),0) FROM state_logs WHERE "+where, args...).Scan(&items[0].Count, &items[0].Bytes); err != nil {
		return nil, err
	}
	if err := s.store.db.QueryRow("SELECT count(*),coalesce(sum(length(value)),0) FROM secrets WHERE id LIKE 'insight-cache:%' OR id LIKE 'insight-image:%'").Scan(&items[3].Count, &items[3].Bytes); err != nil {
		return nil, err
	}
	for _, i := range []int{2, 4} {
		files, e := s.cacheFiles(items[i].Kind, days, st)
		if e != nil {
			return nil, e
		}
		for _, f := range files {
			items[i].Count++
			items[i].Bytes += f.bytes
		}
	}
	return items, nil
}

func (s *Server) cacheAPI(w http.ResponseWriter, r *http.Request, user User) {
	if err := requireAdmin(user); err != nil {
		writeError(w, 403, err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	days := 0
	if r.Method == http.MethodGet {
		switch r.URL.Query().Get("days") {
		case "", "0":
		case "7":
			days = 7
		case "30":
			days = 30
		case "90":
			days = 90
		default:
			writeError(w, 400, "清理范围无效")
			return
		}
		items, err := s.cacheStats(days)
		if err != nil {
			writeError(w, 500, "无法统计缓存，请稍后重试")
			return
		}
		writeJSON(w, 200, items)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, 405, "请求方法不支持")
		return
	}
	var q struct {
		Kind    string `json:"kind"`
		Days    int    `json:"days"`
		Confirm bool   `json:"confirm"`
	}
	if !decode(w, r, &q) {
		return
	}
	if !q.Confirm || (q.Days != 0 && q.Days != 7 && q.Days != 30 && q.Days != 90) {
		writeError(w, 400, "请确认清理类别与范围")
		return
	}
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	s.taskMu.Lock()
	defer s.taskMu.Unlock()
	st := s.store.snapshot()
	for _, t := range st.Tasks {
		if t.Status == "queued" || t.Status == "running" {
			writeError(w, 409, "有任务正在处理，请完成后再清理缓存")
			return
		}
	}
	var count, freed int64
	var err error
	switch q.Kind {
	case "avatars":
		err = s.store.change(func(v *State) error { v.Settings["cache.avatar_version"] = randomID(); return nil })
	case "logs":
		err = s.store.change(func(v *State) error {
			cutoff := time.Now().Add(-time.Duration(q.Days) * 24 * time.Hour)
			kept := []Record{}
			for _, l := range v.Logs {
				t, e := time.Parse(time.RFC3339, l.Time)
				if q.Days == 0 || e == nil && t.Before(cutoff) {
					count++
				} else {
					kept = append(kept, l)
				}
			}
			v.Logs = kept
			return nil
		})
	case "insights":
		var result interface{ RowsAffected() (int64, error) }
		result, err = s.store.db.Exec("DELETE FROM secrets WHERE id LIKE 'insight-cache:%' OR id LIKE 'insight-image:%'")
		if err == nil {
			count, err = result.RowsAffected()
		}
	case "media", "temporary":
		if q.Kind == "media" {
			s.newsCache.Clear()
		}
		var files []cacheFile
		files, err = s.cacheFiles(q.Kind, q.Days, st)
		if err != nil {
			break
		}
		dir := s.cfg.DataDir
		if q.Kind == "media" {
			dir = filepath.Join(dir, "media")
		}
		var root *os.Root
		root, err = os.OpenRoot(dir)
		if os.IsNotExist(err) {
			err = nil
			break
		}
		if err != nil {
			break
		}
		defer root.Close()
		if q.Kind == "media" {
			remove := map[string]bool{}
			for _, f := range files {
				remove[f.name] = true
			}
			err = s.store.change(func(v *State) error {
				if v.Account.ID != st.Account.ID {
					return fmt.Errorf("账号已切换，请重新统计")
				}
				for i := range v.Messages {
					if remove[mediaName(v.Messages[i].Media)] {
						v.Messages[i].Media = ""
					}
				}
				return nil
			})
			if err != nil {
				break
			}
		}
		for _, f := range files {
			if e := root.Remove(f.name); e != nil {
				if !os.IsNotExist(e) {
					err = fmt.Errorf("部分文件无法清理，请刷新后重试")
				}
				continue
			}
			count++
			freed += f.bytes
		}
	default:
		writeError(w, 400, "不支持的缓存类别")
		return
	}
	if err != nil {
		writeError(w, 500, "清理未完成，请刷新后重试；已保存的账号和归档未删除")
		return
	}
	_ = s.store.change(func(v *State) error {
		addLog(v, "INFO", "cache", fmt.Sprintf("%s 清理缓存：%s，%d 项", user.Name, q.Kind, count))
		return nil
	})
	s.notify()
	writeJSON(w, 200, map[string]any{"count": count, "bytes": freed})
}
