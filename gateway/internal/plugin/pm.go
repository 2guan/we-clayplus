package plugin

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/duke-git/lancet/v2/strutil"
	"github.com/pelletier/go-toml/v2"
	"github.com/we-clay/we-clay/sdk/contact"
	"github.com/we-clay/we-clay/sdk/message"
	sdk "github.com/we-clay/we-clay/sdk/plugin"
)

type pmCommandPlugin struct {
	registry *sdk.CommandRegistry
}

type pmLoadCommand struct {
	_    struct{} `cmd:"pm load" help:"加载插件" usage:"/pm load <name>" example:"/pm load example"`
	Name string   `arg:"name" help:"插件名称" required:"true"`
}

type pmUnloadCommand struct {
	_    struct{} `cmd:"pm unload" help:"卸载插件" usage:"/pm unload <name>" example:"/pm unload example"`
	Name string   `arg:"name" help:"插件名称" required:"true"`
}

type pmReloadCommand struct {
	_    struct{} `cmd:"pm reload" help:"重载插件" usage:"/pm reload <name>" example:"/pm reload example"`
	Name string   `arg:"name" help:"插件名称" required:"true"`
}

type pmEnableCommand struct {
	_       struct{} `cmd:"pm enable" help:"启用插件" usage:"/pm enable <name>" example:"/pm enable example"`
	Name    string   `arg:"name" help:"插件名称" required:"true"`
	Command *sdk.Command
}

type pmDisableCommand struct {
	_       struct{} `cmd:"pm disable" help:"禁用插件" usage:"/pm disable <name>" example:"/pm disable example"`
	Name    string   `arg:"name" help:"插件名称" required:"true"`
	Command *sdk.Command
}

type pmListCommand struct {
	_       struct{} `cmd:"pm list" help:"列出已加载插件" usage:"/pm list" example:"/pm list"`
	Command *sdk.Command
}

type pmInfoCommand struct {
	_    struct{} `cmd:"pm info" help:"显示插件元数据" usage:"/pm info <name>" example:"/pm info example"`
	Name string   `arg:"name" help:"插件名称" required:"true"`
}

type pmSetCommand struct {
	_         struct{} `cmd:"pm set" help:"修改插件运行配置" usage:"/pm set <name> [-p priority] [-a true|false] [-n true|false] [-c config]" example:"/pm set example -p 10\n/pm set example -a true -n false\n/pm set example -c \"name='张三'\\nage=18\""`
	Name      string   `arg:"name" help:"插件名称" required:"true"`
	Priority  *int32   `flag:"p,priority" help:"插件优先级"`
	AlwaysRun *bool    `flag:"a,always_run" help:"是否一直运行"`
	Next      *bool    `flag:"n,next" help:"成功后是否继续处理后续插件"`
	Config    *string  `flag:"c,config" help:"TOML 配置字符串"`
}

func registerBuiltinPM() error {
	pm, err := newPMCommandPlugin()
	if err != nil {
		return err
	}
	metadata := &sdk.Metadata{
		Name:        "pm",
		Author:      "we-clay",
		Version:     "builtin",
		Description: "插件管理器",
		Priority:    math.MinInt32,
		Next:        false,
		AlwaysRun:   true,
	}
	w := &wrapper{
		Metadata:       metadata,
		Config:         &Config{Enable: true, Mode: "blacklist"},
		commands:       pm.GetCommands(),
		commandSchemas: pm.GetCommandSchemas(),
		types:          []string{"command", "builtin"},
	}
	w.commandPlugin = new(sdk.CommandPlugin(pm))

	plugins = append(plugins, w)
	sortPlugins()
	rebuildCommandIndex()
	rebuildCapabilityIndex()
	return nil
}

func newPMCommandPlugin() (*pmCommandPlugin, error) {
	pm := &pmCommandPlugin{registry: sdk.NewCommandRegistry()}
	if err := sdk.RegisterCommandTo(pm.registry, pm.load); err != nil {
		return nil, err
	}
	if err := sdk.RegisterCommandTo(pm.registry, pm.unload); err != nil {
		return nil, err
	}
	if err := sdk.RegisterCommandTo(pm.registry, pm.reload); err != nil {
		return nil, err
	}
	if err := sdk.RegisterCommandTo(pm.registry, pm.enable); err != nil {
		return nil, err
	}
	if err := sdk.RegisterCommandTo(pm.registry, pm.disable); err != nil {
		return nil, err
	}
	if err := sdk.RegisterCommandTo(pm.registry, pm.list); err != nil {
		return nil, err
	}
	if err := sdk.RegisterCommandTo(pm.registry, pm.info); err != nil {
		return nil, err
	}
	if err := sdk.RegisterCommandTo(pm.registry, pm.set); err != nil {
		return nil, err
	}
	return pm, nil
}

func (p *pmCommandPlugin) GetCommands() []string {
	return p.registry.Commands()
}

func (p *pmCommandPlugin) GetCommandSchemas() []*sdk.CommandSchema {
	return p.registry.Schemas()
}

func (p *pmCommandPlugin) OnCommand(cmd *sdk.Command) (string, error) {
	return p.registry.Dispatch(cmd)
}

func (p *pmCommandPlugin) load(cmd pmLoadCommand) (string, error) {
	if err := LoadPlugin(cmd.Name); err != nil {
		return "", err
	}
	return fmt.Sprintf("插件已加载：%s", cmd.Name), nil
}

func (p *pmCommandPlugin) unload(cmd pmUnloadCommand) (string, error) {
	if err := UnloadPlugin(cmd.Name); err != nil {
		return "", err
	}
	return fmt.Sprintf("插件已卸载：%s", cmd.Name), nil
}

func (p *pmCommandPlugin) reload(cmd pmReloadCommand) (string, error) {
	if err := ReloadPlugin(cmd.Name); err != nil {
		return "", err
	}
	return fmt.Sprintf("插件已重载：%s", cmd.Name), nil
}

func (p *pmCommandPlugin) enable(cmd pmEnableCommand) (string, error) {
	return p.setEnabled(cmd.Name, cmd.Command, true)
}

func (p *pmCommandPlugin) disable(cmd pmDisableCommand) (string, error) {
	return p.setEnabled(cmd.Name, cmd.Command, false)
}

func (p *pmCommandPlugin) list(cmd pmListCommand) (string, error) {
	items := pluginSnapshot()
	if len(items) == 0 {
		return "当前没有已加载插件", nil
	}

	temp := map[string]string{}
	data := map[string]string{}

	temp["title"] = fmt.Sprintf("插件列表 - 共 %d 插件", len(items))
	temp["desc"] = fmt.Sprintf("共 %d 插件", len(items))

	for _, item := range items {
		state := "启用"
		if item.Config != nil && !item.Config.Enable {
			state = "禁用"
		}

		data[item.Name] = fmt.Sprintf("%s [%s]\n优先级: %d\n类型: %s\n描述: %s", item.Name, state, item.Metadata.Priority, strings.Join(item.types, ", "), item.Description)
	}

	message.Instance.Send(&message.Message{
		Type:     message.TypeAppChatRecord,
		Receiver: cmd.Command.Sender,
		Content:  "插件列表",
		Data: &message.Message_App{App: &message.AppData{
			SubType: 19,
			Title:   temp["title"],
			Desc:    temp["desc"],
			Xml:     buildRecord(temp, data),
		}},
	})
	return "", nil
}

func (p *pmCommandPlugin) info(cmd pmInfoCommand) (string, error) {
	mu.Lock()
	item := findPlugin(cmd.Name)
	mu.Unlock()
	if item == nil {
		return "", fmt.Errorf("插件不存在：%s", cmd.Name)
	}

	lines := []string{
		"插件信息：",
		"名称：" + item.Name,
		"作者：" + item.Author,
		"版本：" + item.Version,
		"描述：" + item.Description,
		fmt.Sprintf("优先级：%d", item.Metadata.Priority),
		fmt.Sprintf("成功后继续：%t", item.Metadata.Next),
		fmt.Sprintf("一直运行：%t", item.Metadata.AlwaysRun),
		"类型：" + strings.Join(item.types, ", "),
		"命令：" + strings.Join(item.commands, ", "),
		"订阅：" + strings.Join(item.subscriptions, ", "),
		"能力：" + strings.Join(item.capabilities, ", "),
		"使用能力：" + strings.Join(item.abilities, ", "),
	}
	if item.Config != nil && item.Config.Config != nil {
		data, err := toml.Marshal(item.Config.Config)
		if err != nil {
			lines = append(lines, "配置：<无法序列化>")
		} else {
			lines = append(lines, "配置：", strings.TrimSpace(string(data)))
		}
	}
	return strings.Join(lines, "\n"), nil
}

func (p *pmCommandPlugin) set(cmd pmSetCommand) (string, error) {
	var changed []string
	var parsedConfig map[string]any
	var pc IPluginConfig

	if cmd.Config != nil {
		value, err := parsePMConfig(*cmd.Config)
		if err != nil {
			return "", err
		}
		parsedConfig = value
	}

	mu.Lock()
	w := findPlugin(cmd.Name)
	if w == nil {
		mu.Unlock()
		return "", fmt.Errorf("插件不存在：%s", cmd.Name)
	}
	if slices.Contains(w.types, "builtin") {
		mu.Unlock()
		return "", fmt.Errorf("内置插件禁止修改：%s", cmd.Name)
	}
	if cmd.Config != nil {
		if !slices.Contains(w.abilities, "config") {
			mu.Unlock()
			return "", fmt.Errorf("插件不支持配置：%s", cmd.Name)
		}
		configPlugin, ok := (*w.plugin).(IPluginConfig)
		if !ok {
			mu.Unlock()
			return "", fmt.Errorf("插件不支持配置：%s", cmd.Name)
		}
		pc = configPlugin
	}
	mu.Unlock()

	if cmd.Config != nil {
		if err := pc.SetConfig([]byte(*cmd.Config)); err != nil {
			return "", fmt.Errorf("注入插件配置失败: %w", err)
		}
	}

	mu.Lock()
	w = findPlugin(cmd.Name)
	if w == nil {
		mu.Unlock()
		return "", fmt.Errorf("插件不存在：%s", cmd.Name)
	}
	if cmd.Priority != nil {
		w.Config.Priority = cmd.Priority
		changed = append(changed, "priority")
	}
	if cmd.AlwaysRun != nil {
		w.Config.AlwaysRun = cmd.AlwaysRun
		changed = append(changed, "always_run")
	}
	if cmd.Next != nil {
		w.Config.Next = cmd.Next
		changed = append(changed, "next")
	}
	if cmd.Config != nil {
		w.Config.Config = parsedConfig
		changed = append(changed, "config")
	}
	applyMetadataConfig(w.Metadata, w.Config)
	sortPlugins()
	rebuildCommandIndex()
	rebuildCapabilityIndex()
	err := saveConfig()
	mu.Unlock()
	if err != nil {
		return "", err
	}

	if len(changed) == 0 {
		return "没有配置被修改", nil
	}
	return fmt.Sprintf("插件配置已修改：%s (%s)", cmd.Name, strings.Join(changed, ", ")), nil
}

func (p *pmCommandPlugin) setEnabled(name string, cmd *sdk.Command, enabled bool) (string, error) {
	mu.Lock()
	w := findPlugin(name)
	if w == nil {
		mu.Unlock()
		return "", fmt.Errorf("插件不存在：%s", name)
	}
	if slices.Contains(w.types, "builtin") {
		mu.Unlock()
		return "", fmt.Errorf("内置插件禁止修改：%s", name)
	}
	if w.Config == nil {
		mu.Unlock()
		return "", fmt.Errorf("插件缺少配置：%s", name)
	}
	isChatroom := cmd != nil && cmd.GetSender().GetType() == contact.ContactType_CONTACT_TYPE_CHATROOM
	if isChatroom {
		sender := cmd.GetSender().GetUsername()
		if sender == "" {
			mu.Unlock()
			return "", fmt.Errorf("命令来源群聊为空")
		}
		if err := setChatroomEnabled(w.Config, sender, enabled); err != nil {
			mu.Unlock()
			return "", err
		}
		err := saveConfig()
		mu.Unlock()
		if err != nil {
			return "", err
		}
		state := "已启用"
		if !enabled {
			state = "已禁用"
		}
		return fmt.Sprintf("插件在当前群聊中%s：%s", state, name), nil
	}
	w.Config.Enable = enabled
	rebuildCommandIndex()
	rebuildCapabilityIndex()
	err := saveConfig()
	mu.Unlock()
	if err != nil {
		return "", err
	}
	if enabled {
		return fmt.Sprintf("插件已启用：%s", name), nil
	}
	return fmt.Sprintf("插件已禁用：%s", name), nil
}

func setChatroomEnabled(cfg *Config, chatroom string, enabled bool) error {
	if cfg == nil {
		return fmt.Errorf("插件缺少配置")
	}
	switch cfg.Mode {
	case "whitelist":
		setLimit(cfg, chatroom, enabled)
	case "", "blacklist":
		cfg.Mode = "blacklist"
		setLimit(cfg, chatroom, !enabled)
	default:
		return fmt.Errorf("插件限制模式不支持：%s", cfg.Mode)
	}
	return nil
}

func setLimit(cfg *Config, value string, present bool) bool {
	exist := slices.Contains(cfg.Limits, value)
	if present {
		if exist {
			return false
		}
		cfg.Limits = append(cfg.Limits, value)
		return true
	}
	if !exist {
		return false
	}
	cfg.Limits = slices.DeleteFunc(cfg.Limits, func(item string) bool {
		return item == value
	})
	return true
}

func parsePMConfig(raw string) (map[string]any, error) {
	var value map[string]any
	if err := toml.Unmarshal([]byte(raw), &value); err != nil {
		return nil, fmt.Errorf("解析 TOML 配置失败: %w", err)
	}
	return value, nil
}

// 构建聊天记录xml
func buildRecord(desc map[string]string, data map[string]string) string {
	self := contact.Instance.GetSelf()
	outter := `<appmsg appid="" sdkver="0">
<title>{title}</title>
<des>{desc}</des><action>view</action><type>19</type><url/><recorditem><![CDATA[<recordinfo>
<title>{title}</title>
<desc>{desc}</desc>
<datalist count="{count}">{data}</datalist>
</recordinfo>]]></recorditem></appmsg>`

	desc["count"] = strconv.Itoa(len(data))
	template := `<dataitem datatype="1" dataid="" htmlid="">
<sourcename>%s</sourcename>
<datadesc>%s</datadesc>
<sourceheadurl>%s</sourceheadurl>
<sourcetime>%s</sourcetime>
<srcMsgLocalid></srcMsgLocalid>
<srcMsgCreateTime>%d</srcMsgCreateTime>
</dataitem>`
	var inner strings.Builder
	for title, data := range data {
		inner.WriteString(fmt.Sprintf(template, title, data, self.Avatar, time.Now().Format(time.DateTime), time.Now().Unix()))
	}
	desc["data"] = inner.String()

	return strutil.TemplateReplace(outter, desc)
}
