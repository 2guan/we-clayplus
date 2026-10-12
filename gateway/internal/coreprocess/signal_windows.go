package coreprocess

import "os"

// Windows 的 os.Process 不支持向任意子进程发送 Interrupt。
func terminate(p *os.Process) error { return p.Kill() }
