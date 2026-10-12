#!/usr/bin/env python3
"""从 proto/ 和 gateway/internal/api/ 重新生成 Go 代码，不修改协议字段。"""
from pathlib import Path
import os, shutil, subprocess

root = Path(__file__).resolve().parents[1]
def tool(name, env):
    path = os.environ.get(env) or shutil.which(name)
    if not path:
        raise SystemExit('缺少生成工具：' + name + '，安装方法见 docs/开发指南.md')
    return path
protoc = tool('protoc', 'PROTOC')
go = tool('protoc-gen-go', 'PROTOC_GEN_GO')
grpc = tool('protoc-gen-go-grpc', 'PROTOC_GEN_GO_GRPC')
for source, output, scan, streaming in [
    (root / 'proto', root / 'sdk', root / 'proto', True),
    (root / 'gateway/internal', root / 'gateway/internal', root / 'gateway/internal/api', False),
]:
    files = sorted(p.relative_to(source).as_posix() for p in scan.rglob('*.proto'))
    args = [protoc, '-I', str(source), '--plugin=protoc-gen-go=' + go,
            '--go_out=' + str(output), '--go_opt=paths=source_relative']
    if streaming:
        args += ['--plugin=protoc-gen-go-grpc=' + grpc,
                 '--go-grpc_out=' + str(output), '--go-grpc_opt=paths=source_relative']
    subprocess.run(args + files, cwd=source, check=True)
    print('已生成', source.relative_to(root), len(files), '个协议文件')
