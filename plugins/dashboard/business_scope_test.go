package dashboard

import (
	"github.com/we-clay/we-clay/plugins/toolbox"
	"testing"
)

func TestBusinessDefaultBlacklistAndModeSemantics(t *testing.T) {
	st := initialState(false)
	for _, d := range toolbox.Catalog {
		if settingText(st, d.ID+".scope") != "blacklist" || settingText(st, d.ID+".targets") != "" {
			t.Fatal("缺省不是空黑名单", d.ID)
		}
		if businessAllowed(st, d.ID, "fixture-peer") {
			t.Fatal("插件关闭仍生效", d.ID)
		}
		for i, p := range st.Plugins {
			if p.ID == d.ID {
				st.Plugins[i].Enabled = true
			}
		}
		for _, target := range []string{"fixture-peer", "fixture-room@chatroom"} {
			if !businessAllowed(st, d.ID, target) {
				t.Fatal("开启后的空黑名单没有全局生效", d.ID)
			}
		}
		st.Settings[d.ID+".targets"] = " fixture-peer , fixture-room@chatroom "
		if businessAllowed(st, d.ID, "fixture-peer") || businessAllowed(st, d.ID, "fixture-room@chatroom") || !businessAllowed(st, d.ID, "fixture-other") {
			t.Fatal("黑名单排除错误", d.ID)
		}
		st.Settings[d.ID+".scope"] = "whitelist"
		if !businessAllowed(st, d.ID, "fixture-peer") || businessAllowed(st, d.ID, "fixture-other") {
			t.Fatal("白名单失效", d.ID)
		}
		st.Settings[d.ID+".targets"] = ""
		if businessAllowed(st, d.ID, "fixture-peer") {
			t.Fatal("空白名单应不生效", d.ID)
		}
	}
}

func TestBusinessScopeDefaultUpgradePersistsAndPreservesManualConfig(t *testing.T) {
	dir := t.TempDir()
	s, e := openStore(dir, false)
	replyRegressionCheck(t, e)
	replyRegressionCheck(t, s.change(func(st *State) error {
		st.PluginScopeDefaultsVersion = 0
		st.Settings["what_to_eat.scope"] = "whitelist"
		st.Settings["what_to_eat.targets"] = ""
		st.Settings["news.scope"] = "whitelist"
		st.Settings["news.targets"] = ""
		st.Settings["music.scope"] = "whitelist"
		st.Settings["music.targets"] = "fixture-peer"
		st.History = append(st.History, Change{ID: "fixture-manual-config", Group: "news"})
		return nil
	}))
	replyRegressionCheck(t, s.db.Close())
	s, e = openStore(dir, false)
	replyRegressionCheck(t, e)
	st := s.snapshot()
	if settingText(st, "what_to_eat.scope") != "blacklist" {
		t.Fatal("旧默认未升级")
	}
	if settingText(st, "news.scope") != "whitelist" || settingText(st, "music.scope") != "whitelist" || settingText(st, "music.targets") != "fixture-peer" {
		t.Fatal("覆盖了既有手动配置")
	}
	replyRegressionCheck(t, s.change(func(st *State) error { st.Settings["what_to_eat.scope"] = "whitelist"; return nil }))
	replyRegressionCheck(t, s.db.Close())
	s, e = openStore(dir, false)
	replyRegressionCheck(t, e)
	defer s.db.Close()
	if settingText(s.snapshot(), "what_to_eat.scope") != "whitelist" || s.snapshot().PluginScopeDefaultsVersion != 1 {
		t.Fatal("重启再次覆盖已选模式")
	}
}
