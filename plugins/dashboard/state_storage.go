// 主要功能：以独立记录增量保存增长集合，配置与记录在同一事务提交。
package dashboard

import (
	"database/sql"
	"encoding/json"
	"reflect"
	"strings"
	"unicode/utf8"
)

const stateStorageVersion = 1

// 字符串不可变，可以共享；只复制可变容器，避免反复编码日志和聊天原文。
func cloneState(v State) State { return cloneValue(reflect.ValueOf(v)).Interface().(State) }

func cloneValue(v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return v
		}
		n := reflect.New(v.Type().Elem())
		n.Elem().Set(cloneValue(v.Elem()))
		return n
	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		n := reflect.New(v.Type()).Elem()
		n.Set(cloneValue(v.Elem()))
		return n
	case reflect.Map:
		if v.IsNil() {
			return v
		}
		n := reflect.MakeMapWithSize(v.Type(), v.Len())
		it := v.MapRange()
		for it.Next() {
			n.SetMapIndex(it.Key(), cloneValue(it.Value()))
		}
		return n
	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		n := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		reflect.Copy(n, v)
		for i := 0; i < n.Len(); i++ {
			n.Index(i).Set(cloneValue(v.Index(i)))
		}
		return n
	case reflect.Struct:
		n := reflect.New(v.Type()).Elem()
		n.Set(v)
		for i := 0; i < n.NumField(); i++ {
			if n.Field(i).CanSet() {
				n.Field(i).Set(cloneValue(v.Field(i)))
			}
		}
		return n
	default:
		return v
	}
}

type stateQuery interface {
	Query(string, ...any) (*sql.Rows, error)
}

func (s *store) initStateRows() error {
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS state_rows(account TEXT NOT NULL,kind TEXT NOT NULL,position INTEGER NOT NULL,body BLOB NOT NULL,PRIMARY KEY(account,kind,position)); CREATE TABLE IF NOT EXISTS state_logs(seq INTEGER PRIMARY KEY AUTOINCREMENT,account TEXT NOT NULL,id TEXT NOT NULL,time TEXT NOT NULL,level TEXT NOT NULL,component TEXT NOT NULL,target TEXT NOT NULL,message TEXT NOT NULL,UNIQUE(account,id)); CREATE INDEX IF NOT EXISTS state_logs_account ON state_logs(account,seq);`)
	return err
}

func storageVersion(raw []byte) int {
	var v struct {
		Version int `json:"storage_version"`
	}
	_ = json.Unmarshal(raw, &v)
	return v.Version
}

func stateDocument(v State) ([]byte, error) {
	v.Contacts, v.Messages, v.Moments, v.Tasks, v.Logs = nil, nil, nil, nil, nil
	return json.Marshal(struct {
		State
		Version int `json:"storage_version"`
	}{v, stateStorageVersion})
}

func loadRows[T any](db stateQuery, account, kind string) ([]T, error) {
	rows, err := db.Query("SELECT body FROM state_rows WHERE account=? AND kind=? ORDER BY position", account, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []T{}
	for rows.Next() {
		var raw []byte
		var item T
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *store) loadCollections(db stateQuery, v *State) error {
	var err error
	if v.Contacts, err = loadRows[Contact](db, v.Account.ID, "contacts"); err != nil {
		return err
	}
	if v.Messages, err = loadRows[Message](db, v.Account.ID, "messages"); err != nil {
		return err
	}
	if v.Moments, err = loadRows[Moment](db, v.Account.ID, "moments"); err != nil {
		return err
	}
	if v.Tasks, err = loadRows[Task](db, v.Account.ID, "tasks"); err != nil {
		return err
	}
	rows, err := db.Query("SELECT id,time,level,component,substr(message,1,512),target FROM state_logs WHERE account=? ORDER BY seq", v.Account.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	v.Logs = []Record{}
	for rows.Next() {
		var r Record
		if err = rows.Scan(&r.ID, &r.Time, &r.Level, &r.Component, &r.Message, &r.Target); err != nil {
			return err
		}
		v.Logs = append(v.Logs, r)
	}
	return rows.Err()
}

func saveRows[T any](tx *sql.Tx, account, kind string, next, old []T) error {
	var stmt *sql.Stmt
	defer func() {
		if stmt != nil {
			_ = stmt.Close()
		}
	}()
	for i, item := range next {
		if i < len(old) && reflect.DeepEqual(item, old[i]) {
			continue
		}
		raw, err := json.Marshal(item)
		if err != nil {
			return err
		}
		if stmt == nil {
			stmt, err = tx.Prepare("INSERT INTO state_rows(account,kind,position,body) VALUES(?,?,?,?) ON CONFLICT(account,kind,position) DO UPDATE SET body=excluded.body")
			if err != nil {
				return err
			}
		}
		if _, err = stmt.Exec(account, kind, i, raw); err != nil {
			return err
		}
	}
	if old == nil || len(next) < len(old) {
		_, err := tx.Exec("DELETE FROM state_rows WHERE account=? AND kind=? AND position>=?", account, kind, len(next))
		return err
	}
	return nil
}

func saveLogs(tx *sql.Tx, account string, next, old []Record) error {
	previous := make(map[string]Record, len(old))
	for _, r := range old {
		previous[r.ID] = r
	}
	for _, r := range next {
		before, exists := previous[r.ID]
		delete(previous, r.ID)
		if exists && before == r {
			continue
		}
		// 启动时内存只有摘要；已存在的原文不能被摘要覆盖。
		query := "INSERT INTO state_logs(account,id,time,level,component,target,message) VALUES(?,?,?,?,?,?,?) ON CONFLICT(account,id) DO NOTHING"
		if exists {
			if before.Message == r.Message {
				if _, err := tx.Exec("UPDATE state_logs SET time=?,level=?,component=?,target=? WHERE account=? AND id=?", r.Time, r.Level, r.Component, r.Target, account, r.ID); err != nil {
					return err
				}
				continue
			}
			query = "INSERT INTO state_logs(account,id,time,level,component,target,message) VALUES(?,?,?,?,?,?,?) ON CONFLICT(account,id) DO UPDATE SET time=excluded.time,level=excluded.level,component=excluded.component,target=excluded.target,message=excluded.message"
		}
		if _, err := tx.Exec(query, account, r.ID, r.Time, r.Level, r.Component, r.Target, r.Message); err != nil {
			return err
		}
	}
	for id := range previous {
		if _, err := tx.Exec("DELETE FROM state_logs WHERE account=? AND id=?", account, id); err != nil {
			return err
		}
	}
	return nil
}

func compactLogs(v *State) {
	for i := range v.Logs {
		body := v.Logs[i].Message
		if len(body) <= 2048 {
			continue
		}
		end := 2048
		for end > 0 && !utf8.RuneStart(body[end]) {
			end--
		}
		v.Logs[i].Message = strings.Clone(body[:end])
	}
}

func (s *store) saveCollections(tx *sql.Tx, next, old State) error {
	if next.Account.ID != old.Account.ID {
		if old.Account.ID == "" {
			// 首次绑定微信账号时把未绑定阶段的完整日志一起迁移，不能只复制摘要。
			for _, r := range next.Logs {
				if _, err := tx.Exec("INSERT OR IGNORE INTO state_logs(account,id,time,level,component,target,message) SELECT ?,id,time,level,component,target,message FROM state_logs WHERE account='' AND id=?", next.Account.ID, r.ID); err != nil {
					return err
				}
			}
		}
		old = State{}
	}
	a := next.Account.ID
	if err := saveRows(tx, a, "contacts", next.Contacts, old.Contacts); err != nil {
		return err
	}
	if err := saveRows(tx, a, "messages", next.Messages, old.Messages); err != nil {
		return err
	}
	if err := saveRows(tx, a, "moments", next.Moments, old.Moments); err != nil {
		return err
	}
	if err := saveRows(tx, a, "tasks", next.Tasks, old.Tasks); err != nil {
		return err
	}
	return saveLogs(tx, a, next.Logs, old.Logs)
}

func (s *store) persistState(tx *sql.Tx, next, old State) error {
	if err := s.saveCollections(tx, next, old); err != nil {
		return err
	}
	raw, err := stateDocument(next)
	if err != nil {
		return err
	}
	_, err = tx.Exec("INSERT INTO documents(id,body) VALUES('state',?) ON CONFLICT(id) DO UPDATE SET body=excluded.body", raw)
	return err
}
