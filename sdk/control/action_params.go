package control

// ActionParams 只由服务端解析授权引用后构造；模型不能提交票据、原始XML或文件路径。
type ActionParams struct {
	LabelMode    string   `json:"label_mode,omitempty"`
	Query        string   `json:"query,omitempty"`
	Offset       int      `json:"offset,omitempty"`
	Members      []string `json:"members,omitempty"`
	Labels       []uint32 `json:"labels,omitempty"`
	Save         bool     `json:"save"`
	Cursor       string   `json:"cursor,omitempty"`
	ReplyComment int32    `json:"reply_comment,omitempty"`
	Kind         string   `json:"kind,omitempty"`
	URL          string   `json:"url,omitempty"`
	Person       string   `json:"person,omitempty"`
	Latitude     float32  `json:"latitude,omitempty"`
	Longitude    float32  `json:"longitude,omitempty"`
	Media        []byte   `json:"media,omitempty"`
	Images       [][]byte `json:"images,omitempty"`
	Blacklist    []string `json:"blacklist,omitempty"`
	WithUsers    []string `json:"with_users,omitempty"`
	Descriptor   []byte   `json:"descriptor,omitempty"`
	V1           string   `json:"v1,omitempty"`
	V2           string   `json:"v2,omitempty"`
	Scene        int      `json:"scene,omitempty"`
}
