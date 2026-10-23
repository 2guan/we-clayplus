package console

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	contactapi "github.com/we-clay/we-clay/gateway/internal/api/contact"
	"github.com/we-clay/we-clay/sdk/control"
	"github.com/we-clay/we-clay/sdk/label"
)

// 添加/移除只改变指定标签；一次一位好友，独立回执可明确追溯部分完成。
func (b *Bridge) modifyAILabels(p control.ActionParams) (any, error) {
	if len(p.Members) != 1 {
		return nil, fmt.Errorf("每次只修改一位好友标签")
	}
	mode := p.LabelMode
	if mode == "" {
		mode = "add"
	}
	if mode != "add" && mode != "remove" && mode != "replace" {
		return nil, fmt.Errorf("标签调整方式无效")
	}
	ids := map[uint32]bool{}
	if mode != "replace" {
		response, e := contactapi.Get().Detail(p.Members)
		if e != nil {
			return nil, e
		}
		known := false
		for _, person := range response.GetContactList() {
			if person.GetUsername().GetValue() != p.Members[0] {
				continue
			}
			if person.LabelIdList == nil {
				continue
			}
			known = true
			for _, raw := range strings.Split(person.GetLabelIdList(), ",") {
				raw = strings.TrimSpace(raw)
				if raw == "" {
					continue
				}
				id, e := strconv.ParseUint(raw, 10, 32)
				if e != nil || id == 0 {
					return nil, fmt.Errorf("现有标签记录无效，未修改")
				}
				ids[uint32(id)] = true
			}
		}
		if !known {
			return nil, fmt.Errorf("未读到现有标签，不能安全合并；只有明确要求完整替换时才能使用replace")
		}
	}
	for _, id := range p.Labels {
		if id == 0 {
			return nil, fmt.Errorf("标签编号无效")
		}
		if mode == "remove" {
			delete(ids, id)
		} else {
			ids[id] = true
		}
	}
	ordered := make([]uint32, 0, len(ids))
	for id := range ids {
		ordered = append(ordered, id)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	raw := []string{}
	for _, id := range ordered {
		raw = append(raw, strconv.FormatUint(uint64(id), 10))
	}
	return label.Instance.ModifyContactLabels(p.Members, strings.Join(raw, ","))
}
