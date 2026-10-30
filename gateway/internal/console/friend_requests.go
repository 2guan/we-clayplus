package console

import (
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/we-clay/we-clay/plugins/dashboard"
	"github.com/we-clay/we-clay/sdk/message"
)

func friendCredentialField(name string) bool {
	n := strings.ToLower(name)
	for _, part := range []string{"ticket", "token", "encrypt", "password", "session", "auth", "key", "buffer", "credential"} {
		if strings.Contains(n, part) {
			return true
		}
	}
	return n == "v1" || n == "v2"
}

// 票据仅交给受控通过流程，所有其余字段完整保留为文字资料，不执行微信提供的 HTML/XML。
func parseFriendRequest(m *message.Message) (dashboard.FriendRequest, error) {
	q := dashboard.FriendRequest{ID: strconv.FormatUint(uint64(m.GetId()), 10), Details: []dashboard.FriendRequestField{}}
	if len(m.GetContent()) > 64<<10 {
		return q, fmt.Errorf("好友申请内容过大")
	}
	var v struct {
		XMLName     xml.Name `xml:"msg"`
		Applicant   string   `xml:"fromusername,attr"`
		Nickname    string   `xml:"fromnickname,attr"`
		V1          string   `xml:"encryptusername,attr"`
		V2          string   `xml:"ticket,attr"`
		Comment     string   `xml:"content,attr"`
		Scene       int      `xml:"scene,attr"`
		BigAvatar   string   `xml:"bigheadimgurl,attr"`
		SmallAvatar string   `xml:"smallheadimgurl,attr"`
	}
	if err := xml.Unmarshal([]byte(m.GetContent()), &v); err != nil {
		return q, err
	}
	q.Applicant, q.Nickname, q.V1, q.V2, q.Comment, q.Scene = v.Applicant, v.Nickname, v.V1, v.V2, v.Comment, v.Scene
	q.Avatar = safeWebURL(v.BigAvatar)
	if q.Avatar == "" {
		q.Avatar = safeWebURL(v.SmallAvatar)
	}
	if m.GetTimestamp() > 0 {
		q.Created = time.Unix(int64(m.GetTimestamp()), 0).Format(time.RFC3339)
	}
	decoder := xml.NewDecoder(strings.NewReader(m.GetContent()))
	path := []string{}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return q, err
		}
		switch t := token.(type) {
		case xml.StartElement:
			if friendCredentialField(t.Name.Local) {
				if err = decoder.Skip(); err != nil {
					return q, err
				}
				continue
			}
			path = append(path, t.Name.Local)
			for _, a := range t.Attr {
				if !friendCredentialField(a.Name.Local) {
					q.Details = append(q.Details, dashboard.FriendRequestField{Name: strings.Join(path, "/") + "/@" + a.Name.Local, Value: a.Value})
				}
			}
		case xml.EndElement:
			if len(path) > 0 {
				path = path[:len(path)-1]
			}
		case xml.CharData:
			if text := strings.TrimSpace(string(t)); text != "" {
				q.Details = append(q.Details, dashboard.FriendRequestField{Name: strings.Join(path, "/"), Value: text})
			}
		}
	}
	return q, nil
}
