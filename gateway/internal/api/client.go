package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/we-clay/we-clay/sdk/corewire"
	"google.golang.org/grpc"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// Client calls the separate core process without retrying business requests.
type Client struct {
	token string
	conn  *grpc.ClientConn
	rpc   corewire.CoreServiceClient
	ctx   context.Context
}

type Request struct {
	ctx                      context.Context
	client                   *Client
	method, uri, contentType string
	query                    url.Values
	body                     []byte
	err                      error
	binary                   bool
}

func (r *Request) WithContext(ctx context.Context) *Request { r.ctx = ctx; return r }

type FilePart struct {
	Name, Filename string
	Data           []byte
}

var GetCore = func() *Client { return &Client{} }

func (c *Client) NewRequest(method, uri string) *Request {
	return &Request{client: c, method: method, uri: uri}
}
func (c *Client) Get(uri string) *Request    { return c.NewRequest(http.MethodGet, uri) }
func (c *Client) Post(uri string) *Request   { return c.NewRequest(http.MethodPost, uri) }
func (c *Client) Put(uri string) *Request    { return c.NewRequest(http.MethodPut, uri) }
func (c *Client) Delete(uri string) *Request { return c.NewRequest(http.MethodDelete, uri) }
func (r *Request) Query(args ...any) *Request {
	if len(args)%2 != 0 {
		r.err = errors.New("query requires key/value pairs")
		return r
	}
	if r.query == nil {
		r.query = make(url.Values)
	}
	for i := 0; i < len(args); i += 2 {
		r.query.Set(fmt.Sprint(args[i]), fmt.Sprint(args[i+1]))
	}
	return r
}
func (r *Request) Path(path string) *Request {
	r.uri = fmt.Sprintf(r.uri, url.PathEscape(path))
	return r
}
func (r *Request) Body(body any) *Request {
	if r.err != nil {
		return r
	}
	r.body, r.err = json.Marshal(body)
	r.contentType = "application/json"
	return r
}
func (r *Request) Multipart(files map[string][]byte, fields map[string]string) *Request {
	parts := make([]FilePart, 0, len(files))
	for name, data := range files {
		parts = append(parts, FilePart{Name: name, Filename: name, Data: data})
	}
	return r.MultipartFiles(parts, fields)
}
func (r *Request) MultipartFiles(files []FilePart, fields map[string]string) *Request {
	if r.err != nil {
		return r
	}
	var buffer bytes.Buffer
	w := multipart.NewWriter(&buffer)
	for _, file := range files {
		if file.Name == "" || file.Filename == "" || strings.ContainsAny(file.Filename, "\r\n") {
			r.err = errors.New("invalid upload filename")
			return r
		}
		part, err := w.CreateFormFile(file.Name, file.Filename)
		if err != nil {
			r.err = err
			return r
		}
		if _, err = part.Write(file.Data); err != nil {
			r.err = err
			return r
		}
	}
	for name, value := range fields {
		if err := w.WriteField(name, value); err != nil {
			r.err = err
			return r
		}
	}
	if err := w.Close(); err != nil {
		r.err = err
		return r
	}
	r.body = buffer.Bytes()
	r.contentType = w.FormDataContentType()
	return r
}
func (r *Request) Do() ([]byte, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.doGRPC()
}

// DoBinary 要求真实的媒体回执，拒绝把成功 JSON 信封保存成文件。
func (r *Request) DoBinary() ([]byte, error) {
	r.binary = true
	return r.Do()
}
func decodeResponse(code int, contentType string, raw []byte) ([]byte, error) {
	if code < 200 || code >= 300 {
		// 只提取约定错误信封中的说明，不把 HTML、媒体或完整响应体写入错误。
		var problem struct {
			Code    *int64 `json:"code"`
			Message string `json:"message"`
		}
		if len(raw) <= 64<<10 && json.Unmarshal(raw, &problem) == nil && problem.Code != nil && *problem.Code != 0 && problem.Message != "" {
			message := []rune(problem.Message)
			if len(message) > 1024 {
				message = append(message[:1024], '…')
			}
			return nil, fmt.Errorf("core returned status %d (code %d): %s", code, *problem.Code, string(message))
		}
		return nil, fmt.Errorf("core returned status %d", code)
	}
	if strings.HasPrefix(contentType, "application/octet-stream") {
		return raw, nil
	}
	var envelope struct {
		Code    *int64          `json:"code"`
		Data    json.RawMessage `json:"data"`
		Message string          `json:"message"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil || envelope.Code == nil {
		return nil, errors.New("invalid core response envelope")
	}
	if *envelope.Code != 0 {
		return nil, fmt.Errorf("core rejected request (code %d): %s", *envelope.Code, envelope.Message)
	}
	return envelope.Data, nil
}
func (r *Request) DoProto(result proto.Message, allowedCodes ...int32) error {
	raw, err := r.Do()
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return errors.New("core response data missing; result is unconfirmed")
	}
	if err = (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(raw, result); err == nil {
		return validateReceipt(result, r.uri, allowedCodes)
	}
	proto.Reset(result)
	if err = json.Unmarshal(raw, result); err != nil {
		return errors.New("invalid core response data")
	}
	return validateReceipt(result, r.uri, allowedCodes)
}
func (r *Request) DoJson(result any) error {
	raw, err := r.Do()
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, result)
}
