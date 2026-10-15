#!/usr/bin/env bash
# 发布时由 Action 把版本占位符替换为当前不可变 tag，并上传到私有 OSS。
set -euo pipefail
umask 077

version='@WE_CLAY_VERSION@'
auth_url='https://nww.hihfy.com/we-clay-auth/v1/download'
bucket_host='weclay.oss-cn-beijing.aliyuncs.com'

usage() {
  cat <<'EOF'
用法：install.sh [--key-file 授权文件] [--data-dir 本机目录] [--core-data-dir 旧微信账号目录] [--no-run]

不传 --key-file 时读取已安装的授权文件；首次安装会安全提示输入 Key。
已有微信登录数据时，必须传 --core-data-dir 指向原来的 core 账号目录。
EOF
}

fail() { printf '安装失败：%s\n' "$1" >&2; exit 1; }

key_file=''
data_dir=''
core_data_dir=''
run_after=1
while (($#)); do
  case "$1" in
    --key-file|--data-dir|--core-data-dir)
      (($# >= 2)) || fail "$1 缺少路径"
      case "$1" in
        --key-file) key_file=$2 ;;
        --data-dir) data_dir=$2 ;;
        --core-data-dir) core_data_dir=$2 ;;
      esac
      shift 2 ;;
    --no-run) run_after=0; shift ;;
    -h|--help) usage; exit 0 ;;
    *) fail "不支持的参数：$1" ;;
  esac
done

[[ "$version" =~ ^v(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})$ ]] || fail '安装脚本尚未经过正式发布构建'
command -v curl >/dev/null || fail '系统缺少 curl'
case "$(uname -s)" in
  Darwin)
    goos=darwin
    : "${data_dir:=$HOME/Library/Application Support/we-clay}"
    command -v shasum >/dev/null || fail '系统缺少 shasum'
    hash_file() { shasum -a 256 "$1" | cut -d ' ' -f 1; }
    ;;
  Linux)
    goos=linux
    : "${data_dir:=${XDG_DATA_HOME:-$HOME/.local/share}/we-clay}"
    command -v sha256sum >/dev/null || fail '系统缺少 sha256sum'
    hash_file() { sha256sum "$1" | cut -d ' ' -f 1; }
    ;;
  *) fail '仅支持 macOS 和 Linux；Windows 请使用 install.ps1' ;;
esac
case "$(uname -m)" in
  arm64|aarch64) goarch=arm64 ;;
  x86_64|amd64) goarch=amd64 ;;
  *) fail '当前 CPU 架构尚无发布文件' ;;
esac
[[ "$data_dir" = /* ]] || fail '--data-dir 必须是绝对路径'
if [[ -n "$core_data_dir" ]]; then
  [[ "$core_data_dir" = /* && -d "$core_data_dir" ]] || fail '--core-data-dir 必须指向已存在的绝对目录，避免误建新设备'
fi

mkdir -p "$data_dir/secrets" "$data_dir/bin"
existing_install=0
[[ -f "$data_dir/installed-version.txt" ]] && existing_install=1
license_file="$data_dir/secrets/license.key"
if [[ -n "$key_file" ]]; then
  [[ -f "$key_file" ]] || fail '授权文件不存在'
  key="$(cat "$key_file")"
elif [[ -f "$license_file" ]]; then
  key="$(cat "$license_file")"
else
  [[ -r /dev/tty ]] || fail '请在交互终端安装，或使用 --key-file 提供授权文件'
  printf '请输入授权 Key（不回显）：' >&2
  IFS= read -r -s key < /dev/tty
  printf '\n' >&2
fi
[[ "$key" =~ ^wca_[0-9a-f]{64}$ ]] || fail '授权 Key 格式无效'

request=$(printf '{"product":"we-clay","version":"%s","goos":"%s","goarch":"%s","asset":"bundle"}' "$version" "$goos" "$goarch")
plan_file=$(mktemp "$data_dir/bin/.download-plan.XXXXXX")
binary_tmp=$(mktemp "$data_dir/bin/.we-clay.XXXXXX")
trap 'rm -f "$plan_file" "$binary_tmp"' EXIT

# curl 从 stdin 接收含 Key 的配置，Key 不出现在进程参数或日志中。
if ! printf 'header = "Authorization: Bearer %s"\n' "$key" | curl --config - --silent --fail --show-error --proto '=https' --tlsv1.2 --max-time 30 \
  --header 'Accept: text/plain' --header 'Content-Type: application/json' \
  --request POST --data "$request" --output "$plan_file" "$auth_url" 2>/dev/null; then
  fail '授权服务拒绝下载或暂时不可用；请检查 Key、有效期和网络'
fi
[[ $(wc -l < "$plan_file" | tr -d ' ') == 4 ]] || fail '授权服务返回格式错误'
exec 3< "$plan_file"
IFS= read -r resolved_version <&3
IFS= read -r asset_url <&3
IFS= read -r expected_hash <&3
IFS= read -r expected_size <&3
exec 3<&-
[[ "$resolved_version" = "$version" ]] || fail '下载版本与安装脚本不一致'
[[ "$expected_hash" =~ ^[0-9a-f]{64}$ && "$expected_size" =~ ^[0-9]+$ ]] || fail '下载摘要或大小无效'
asset_name="we-clay_${version}_${goos}_${goarch}"
case "$asset_url" in
  "https://${bucket_host}/we-clay/${version}/${asset_name}?"*) ;;
  *) fail '授权服务返回了非预期的 OSS 下载地址' ;;
esac
[[ "$asset_url" =~ ^[A-Za-z0-9._~:/?\&=%+-]+$ ]] || fail '下载地址包含不安全字符'

# 私有 OSS 的短时 URL 只从 stdin 传给 curl，不进入命令行参数；不跟随重定向。
if ! printf 'url = "%s"\n' "$asset_url" | curl --config - --silent --fail --show-error --proto '=https' --tlsv1.2 \
  --max-time 600 --output "$binary_tmp" 2>/dev/null; then
  fail '二进制下载失败；短时地址可能已过期，请重新运行安装脚本'
fi
actual_size=$(wc -c < "$binary_tmp" | tr -d ' ')
[[ "$actual_size" = "$expected_size" ]] || fail '二进制大小校验失败'
[[ "$(hash_file "$binary_tmp")" = "$expected_hash" ]] || fail '二进制 SHA-256 校验失败'
chmod 0700 "$binary_tmp"
destination="$data_dir/bin/we-clay_${version}"
if [[ -f "$destination" ]]; then
  [[ "$(hash_file "$destination")" = "$expected_hash" ]] || fail '同版本本地二进制与已发布摘要不一致'
  rm -f "$binary_tmp"
else
  mv "$binary_tmp" "$destination"
fi
binary_tmp=''

# 原设备路径独立保存为数据，不把用户输入拼接进可执行脚本。
if [[ -n "$core_data_dir" ]]; then
  old_core_tmp=$(mktemp "$data_dir/.core-data-dir.XXXXXX")
  printf '%s\n' "$core_data_dir" > "$old_core_tmp"
  chmod 0600 "$old_core_tmp"
  mv -f "$old_core_tmp" "$data_dir/core-data-dir.txt"
fi
runner_tmp=$(mktemp "$data_dir/.run.XXXXXX")
cat > "$runner_tmp" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
IFS= read -r version < "$root/installed-version.txt"
[[ "$version" =~ ^v(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})$ ]] || { printf '安装版本文件无效\n' >&2; exit 1; }
args=(--data-dir "$root" --license-file "$root/secrets/license.key")
if [[ -f "$root/core-data-dir.txt" ]]; then
  IFS= read -r old_core < "$root/core-data-dir.txt"
  args+=(--core-data-dir "$old_core")
fi
exec "$root/bin/we-clay_${version}" "${args[@]}" "$@"
EOF
chmod 0700 "$runner_tmp"
mv -f "$runner_tmp" "$data_dir/run.sh"
if [[ ! -f "$license_file" || -n "$key_file" ]]; then
  secret_tmp=$(mktemp "$data_dir/secrets/.license.XXXXXX")
  printf '%s\n' "$key" > "$secret_tmp"
  chmod 0600 "$secret_tmp"
  mv -f "$secret_tmp" "$license_file"
fi
chmod 0600 "$license_file"
key=''
version_tmp=$(mktemp "$data_dir/.installed-version.XXXXXX")
printf '%s\n' "$version" > "$version_tmp"
chmod 0600 "$version_tmp"
mv -f "$version_tmp" "$data_dir/installed-version.txt"
printf '安装完成：we-clay %s\n运行入口：%s/run.sh\n控制台：http://127.0.0.1:8208\n' "$version" "$data_dir"
if ((existing_install)); then
  printf '检测到升级：请先停止旧 gateway，再运行 %s/run.sh；独立 core 会保持微信登录。\n' "$data_dir"
  run_after=0
fi
if ((run_after)); then
  exec "$data_dir/run.sh"
fi
