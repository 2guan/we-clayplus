// Package labelability 提供标签能力的实现。
package labelability

import (
	"errors"
	sdk "github.com/we-clay/we-clay/sdk/label"

	labelapi "github.com/we-clay/we-clay/gateway/internal/api/label"
)

// ability 标签能力实现（直连型）
type ability struct {
	api labelapi.LabelService
}

func init() {
	sdk.Instance = &ability{api: labelapi.Get()}
}

// List 获取标签列表
func (a ability) List() ([]*sdk.LabelPair, error) {
	resp, err := a.api.List()
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("label 回执缺失，结果未确认")
	}
	labels := make([]*sdk.LabelPair, 0, len(resp.List))
	for _, l := range resp.List {
		labels = append(labels, mapPair(l))
	}
	return labels, nil
}

// Add 添加标签
func (a ability) Add(name string) (*sdk.LabelPair, error) {
	resp, err := a.api.Add(name)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("label 回执缺失，结果未确认")
	}
	if resp.GetList().GetId() != 0 {
		return mapPair(resp.List), nil
	}
	return nil, errors.New("标签添加回执缺少标签 ID，结果未确认，请勿自动重试")
}

// Delete 删除标签
func (a ability) Delete(labelIds string) (*sdk.Delete_Response, error) {
	resp, err := a.api.Delete(labelIds)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("label 回执缺失，结果未确认")
	}
	return &sdk.Delete_Response{
		Code:    resp.GetBaseResponse().GetCode(),
		Message: resp.GetBaseResponse().GetMessage().GetValue(),
	}, nil
}

// Update 更新标签名称
func (a ability) Update(labelId uint32, name string) (*sdk.Update_Response, error) {
	resp, err := a.api.Update(labelId, name)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("label 回执缺失，结果未确认")
	}
	return &sdk.Update_Response{
		Code:    resp.GetBaseResponse().GetCode(),
		Message: resp.GetBaseResponse().GetMessage().GetValue(),
	}, nil
}

// ModifyContactLabels 修改联系人标签
func (a ability) ModifyContactLabels(usernames []string, labelIds string) (*sdk.ModifyContact_Response, error) {
	resp, err := a.api.ModifyContactLabels(usernames, labelIds)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("label 回执缺失，结果未确认")
	}
	return &sdk.ModifyContact_Response{
		Code:    resp.GetBaseResponse().GetCode(),
		Message: resp.GetBaseResponse().GetMessage().GetValue(),
	}, nil
}

func mapPair(pair *labelapi.Pair) *sdk.LabelPair {
	if pair == nil {
		return nil
	}
	return &sdk.LabelPair{
		Id:   pair.GetId(),
		Name: pair.GetName(),
	}
}
