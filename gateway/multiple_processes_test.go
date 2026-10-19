package gateway_test

import (
	"testing"
	"time"
)

// 一个测试进程顺序连接两套 gateway；各自独立 core 保持运行，模拟两个应用的隔离部署。
func TestTwoIndependentCoresKeepSeparatePortsAndState(t *testing.T) {
	a := managedFixture(t, "normal")
	a.Core.KeepAlive = true
	a.CoreToken = ""
	a.CoreTokenEnv = "-"
	a.Core.StartupTimeout = 15 * time.Second
	a.Core.LoginTimeout = 15 * time.Second
	cleanupIndependent(t, a)
	b := managedFixture(t, "normal")
	b.Core.KeepAlive = true
	b.CoreToken = ""
	b.CoreTokenEnv = "-"
	b.Core.StartupTimeout = 15 * time.Second
	b.Core.LoginTimeout = 15 * time.Second
	cleanupIndependent(t, b)
	independentRound(t, a)
	pidA, tokenA := independentRecord(t, a)
	independentRound(t, b)
	pidB, tokenB := independentRecord(t, b)
	if pidA == pidB || tokenA == tokenB || a.CoreAddress == b.CoreAddress || a.Core.DataDir == b.Core.DataDir {
		t.Fatal("core instances were not isolated")
	}
	independentRound(t, a)
	again, token := independentRecord(t, a)
	if again != pidA || token != tokenA {
		t.Fatal("starting second instance replaced first core")
	}
}
