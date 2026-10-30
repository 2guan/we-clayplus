#!/usr/bin/env bash
# 发布时由 Action 把版本占位符替换为当前不可变 tag，并上传到私有 OSS。
set -euo pipefail
umask 077

version='@WE_CLAY_VERSION@'
auth_url='https://nww.hihfy.com/we-clay-auth/v1/download'
bucket_host='weclay.oss-cn-beijing.aliyuncs.com'

usage() {
  cat <<'EOF'
用法：install.sh [--key-file 授权文件] [--data-dir 本机目录] [--core-data-dir 旧微信账号目录]
                [--core-version vX.Y.Z] [--core-address 本机地址] [--login-address 本机地址]
                [--dashboard-address 本机地址] [--device-type mac|win] [--device-name 微信设备名称]
                [--previous-core-version vX.Y.Z] [--previous-core-address 本机地址]
                [--previous-login-address 本机地址] [--previous-dashboard-address 本机地址]
                [--previous-core-data-dir 旧微信账号目录]
                [--restart | --no-run]

不传 --key-file 时读取已安装的授权文件；首次安装会安全提示输入 Key。
已有微信登录数据时，必须传 --core-data-dir 指向原来的 core 账号目录。
--device-type 默认 mac；新 Windows 设备用 win，须使用独立新安装目录和账号目录。
已有安装不可切换设备类型，旧安装缺少类型配置时按 mac 保留。
--device-name 可选；不传时沿用此前保存的名称或 core 中的原设备名称。
--restart 用于已安装实例：校验后保存新配置并运行；需要切换 core 配置时才受控停止旧 core。
执行 --restart 前先结束旧 gateway；脚本不会结束来源不明的进程。
旧安装曾手工传过 core 版本或地址时，请用 --previous-* 填写旧运行参数。
EOF
}

fail() { printf '安装失败：%s\n' "$1" >&2; exit 1; }
stage() { printf '%s\n' "$1" >&2; }

key_file=''
data_dir=''
core_data_dir=''
device_type=''
device_type_set=0
device_name=''
device_name_set=0
core_version=''
core_address=''
login_address=''
dashboard_address=''
previous_core_version=''
previous_core_address=''
previous_login_address=''
previous_dashboard_address=''
previous_core_data_dir=''
restart=0
run_after=1
while (($#)); do
  case "$1" in
    --key-file|--data-dir|--core-data-dir|--device-type|--device-name|--core-version|--core-address|--login-address|--dashboard-address|--previous-core-version|--previous-core-address|--previous-login-address|--previous-dashboard-address|--previous-core-data-dir)
      (($# >= 2)) || fail "$1 缺少参数值"
      case "$1" in
        --key-file) key_file=$2 ;;
        --data-dir) data_dir=$2 ;;
        --core-data-dir) core_data_dir=$2 ;;
        --device-type) device_type=$2; device_type_set=1 ;;
        --device-name) device_name=$2; device_name_set=1 ;;
        --core-version) core_version=$2 ;;
        --core-address) core_address=$2 ;;
        --login-address) login_address=$2 ;;
        --dashboard-address) dashboard_address=$2 ;;
        --previous-core-version) previous_core_version=$2 ;;
        --previous-core-address) previous_core_address=$2 ;;
        --previous-login-address) previous_login_address=$2 ;;
        --previous-dashboard-address) previous_dashboard_address=$2 ;;
        --previous-core-data-dir) previous_core_data_dir=$2 ;;
      esac
      shift 2 ;;
    --restart) restart=1; shift ;;
    --no-run) run_after=0; shift ;;
    -h|--help) usage; exit 0 ;;
    *) fail "不支持的参数：$1" ;;
  esac
done
(( !(restart && !run_after) )) || fail '--restart 与 --no-run 不能同时使用'

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
if ((device_name_set)); then
  [[ -n "${device_name//[[:space:]]/}" && "$device_name" != *$'\n'* && "$device_name" != *$'\r'* ]] || fail '--device-name 不能为空、纯空白或包含换行'
fi

version_ok() { [[ "$1" =~ ^v(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})$ ]]; }
address_ok() {
  local address=$1 port
  [[ "$address" =~ ^(127\.0\.0\.1|\[::1\]):([1-9][0-9]{0,4})$ ]] || return 1
  port=${BASH_REMATCH[2]}
  (( 10#$port <= 65535 ))
}
read_setting() {
  local value
  [[ -f "$1" ]] || { printf '%s' "$2"; return; }
  value=$(cat "$1")
  [[ -n "$value" && "$value" != *$'\n'* && "$value" != *$'\r'* ]] || fail "本地配置文件无效：${1##*/}"
  printf '%s' "$value"
}
write_setting() {
  local temporary
  temporary=$(mktemp "$data_dir/.${1}.XXXXXX")
  printf '%s\n' "$2" > "$temporary"
  chmod 0600 "$temporary"
  mv -f "$temporary" "$data_dir/$1"
}
port_in_use() {
  # curl 的 num_connects 统计 TCP 握手；即使服务端重置或返回非 HTTP 内容也能识别占用。
  local connections curl_status=0
  connections=$(curl --noproxy '*' --silent --output /dev/null --connect-timeout 1 --max-time 2 \
    --write-out '%{num_connects}' "http://$1/" 2>/dev/null) || curl_status=$?
  [[ "$connections" = 1 ]] && return 0
  [[ "$connections" = 0 && "$curl_status" = 7 ]] && return 1
  fail '无法确认旧控制台端口是否仍在监听；请检查本机网络后重试'
}

existing_install=0
[[ -f "$data_dir/installed-version.txt" ]] && existing_install=1
(( !restart || existing_install )) || fail '--restart 只用于已有安装；首次安装请直接运行安装脚本'
# 旧安装可能尚未保存 core-version.txt；该场景必须按原默认值核验旧进程。
core_version_fallback='v0.5.2'
if ((existing_install)); then core_version_fallback='v0.4.5'; fi
old_core_version=$(read_setting "$data_dir/core-version.txt" "$core_version_fallback")
old_core_address=$(read_setting "$data_dir/core-address.txt" '127.0.0.1:8088')
old_login_address=$(read_setting "$data_dir/login-address.txt" '127.0.0.1:8201')
old_dashboard_address=$(read_setting "$data_dir/dashboard-address.txt" '127.0.0.1:8208')
old_device_type=$(read_setting "$data_dir/device-type.txt" 'mac')
[[ "$old_device_type" = mac || "$old_device_type" = win ]] || fail '已保存的设备类型必须为 mac 或 win'
if (( !device_type_set )); then device_type=$old_device_type; fi
[[ "$device_type" = mac || "$device_type" = win ]] || fail '--device-type 必须为 mac 或 win'
if ((existing_install)) && [[ "$device_type" != "$old_device_type" ]]; then
  fail '已有安装不能切换设备类型；请为 Windows 设备使用独立新安装目录和账号目录'
fi
if [[ -n "$previous_core_version" ]]; then old_core_version=$previous_core_version; fi
if [[ -n "$previous_core_address" ]]; then old_core_address=$previous_core_address; fi
if [[ -n "$previous_login_address" ]]; then old_login_address=$previous_login_address; fi
if [[ -n "$previous_dashboard_address" ]]; then old_dashboard_address=$previous_dashboard_address; fi
if [[ -z "$core_version" ]]; then core_version=$old_core_version; fi
if [[ -z "$core_address" ]]; then core_address=$old_core_address; fi
if [[ -z "$login_address" ]]; then login_address=$old_login_address; fi
if [[ -z "$dashboard_address" ]]; then dashboard_address=$old_dashboard_address; fi
version_ok "$old_core_version" && version_ok "$core_version" || fail 'core 版本必须是已发布的固定 vX.Y.Z'
address_ok "$old_core_address" && address_ok "$core_address" || fail 'core 地址必须是 127.0.0.1:端口或 [::1]:端口'
[[ "$old_login_address" = '-' ]] || address_ok "$old_login_address" || fail '旧登录地址无效'
[[ "$login_address" = '-' ]] || address_ok "$login_address" || fail '登录地址无效'
address_ok "$old_dashboard_address" && address_ok "$dashboard_address" || fail '控制台地址必须是 127.0.0.1:端口或 [::1]:端口'
[[ "$core_address" != "$login_address" && "$core_address" != "$dashboard_address" && "$login_address" != "$dashboard_address" ]] || fail 'core、登录和控制台地址不能重复'
saved_core_data_dir=$(read_setting "$data_dir/core-data-dir.txt" "$data_dir/core")
old_core_data_dir=$saved_core_data_dir
if [[ -n "$previous_core_data_dir" ]]; then
  [[ "$previous_core_data_dir" = /* && -d "$previous_core_data_dir" ]] || fail '--previous-core-data-dir 必须指向已存在的绝对目录'
  old_core_data_dir=$previous_core_data_dir
  [[ -n "$core_data_dir" ]] || core_data_dir=$old_core_data_dir
fi
if ((existing_install)) && [[ -n "$core_data_dir" && "$core_data_dir" != "$old_core_data_dir" ]]; then
  fail '已有安装不能直接切换账号目录；请先按原配置停止 core 并迁移账号目录'
fi
path_override=0
if ((existing_install)) && [[ "$old_core_data_dir" != "$saved_core_data_dir" ]]; then
  path_override=1
  [[ -f "$old_core_data_dir/gateway-core.json" && ! -L "$old_core_data_dir/gateway-core.json" ]] || \
    fail '指定的旧账号目录缺少独立 core 记录，无法核验运行中的原设备'
fi
old_device_name=$(read_setting "$data_dir/device-name.txt" '')
stage "准备安装 we-clay ${version}（${goos}/${goarch}）"

mkdir -p "$data_dir/secrets" "$data_dir/bin"
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
old_key=''
if [[ -f "$license_file" ]]; then old_key=$(cat "$license_file"); fi
need_stop=0
if ((existing_install)) && { ((path_override)) || [[ "$core_version" != "$old_core_version" || "$core_address" != "$old_core_address" || "$login_address" != "$old_login_address" || "$dashboard_address" != "$old_dashboard_address" || "$key" != "$old_key" ]] || { ((device_name_set)) && [[ "$device_name" != "$old_device_name" ]]; }; }; then
  need_stop=1
  [[ -d "$old_core_data_dir" ]] || fail '原账号目录不存在；请核对已有安装的 core 数据目录'
fi

request=$(printf '{"product":"we-clay","version":"%s","goos":"%s","goarch":"%s","asset":"bundle"}' "$version" "$goos" "$goarch")
plan_file=$(mktemp "$data_dir/bin/.download-plan.XXXXXX")
binary_tmp=$(mktemp "$data_dir/bin/.we-clay.XXXXXX")
candidate_license_tmp=''
download_pid=''
cleanup() {
  if [[ -n "$download_pid" ]]; then
    kill "$download_pid" 2>/dev/null || true
  fi
  rm -f "$plan_file" "$binary_tmp"
  [[ -z "$candidate_license_tmp" ]] || rm -f "$candidate_license_tmp"
}
trap cleanup EXIT

# curl 从 stdin 接收含 Key 的配置，Key 不出现在进程参数或日志中。
stage '正在验证授权并申请下载地址…'
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
stage '正在下载主程序…'
printf 'url = "%s"\n' "$asset_url" | curl --config - --silent --fail --show-error --proto '=https' --tlsv1.2 \
  --max-time 600 --output "$binary_tmp" 2>/dev/null &
download_pid=$!
show_download_progress() {
  local current_size progress
  current_size=$(wc -c < "$binary_tmp" | tr -d ' ')
  progress=$(awk -v current="$current_size" -v total="$expected_size" 'BEGIN {
    percent = total > 0 ? int(current / total * 100) : 0
    if (percent > 100) percent = 100
    printf "%3d%%  %.1f / %.1f MiB", percent, current / 1048576, total / 1048576
  }')
  printf '\r下载进度：%s' "$progress" >&2
}
if [[ -t 2 ]]; then
  while kill -0 "$download_pid" 2>/dev/null; do
    show_download_progress
    sleep 1
  done
fi
download_ok=1
wait "$download_pid" || download_ok=0
download_pid=''
if [[ -t 2 ]]; then
  if ((download_ok)); then
    show_download_progress
    printf '\n' >&2
  else
    printf '\r\033[K' >&2
  fi
fi
if (( !download_ok )); then
  fail '二进制下载失败；短时地址可能已过期，请重新运行安装脚本'
fi
stage '下载完成，正在校验文件大小与 SHA-256…'
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
if ((need_stop)); then
  stage '正在预备并校验目标 core…'
  candidate_license_tmp=$(mktemp "$data_dir/secrets/.candidate.XXXXXX")
  printf '%s\n' "$key" > "$candidate_license_tmp"
  chmod 0600 "$candidate_license_tmp"
  if ! "$destination" --prepare-core --data-dir "$data_dir" --core-data-dir "$old_core_data_dir" \
      --core-version "$core_version" --device-type "$device_type" --core-address "$core_address" --login-address "$login_address" \
      --dashboard-address "$dashboard_address" --license-file "$candidate_license_tmp"; then
    fail '目标 core 下载或校验失败；旧 core 与本地授权配置未修改'
  fi
  rm -f "$candidate_license_tmp"
  candidate_license_tmp=''
  if port_in_use "$old_dashboard_address"; then
    fail '旧 gateway 仍在监听控制台端口；请先正常结束旧 gateway，再重新执行安装脚本'
  fi
  stage '正在核验并停止旧独立 core…'
  if ! "$destination" --stop-core --data-dir "$data_dir" --core-data-dir "$old_core_data_dir" \
      --core-version "$old_core_version" --device-type "$old_device_type" --core-address "$old_core_address" --login-address "$old_login_address" \
      --dashboard-address "$old_dashboard_address"; then
    fail '未能确认并停止旧 core；原授权与配置未修改，请核对旧版本和地址'
  fi
fi
if ((restart && !need_stop)) && port_in_use "$old_dashboard_address"; then
  fail '旧 gateway 仍在监听控制台端口；请先正常结束旧 gateway，再重新执行安装脚本'
fi
stage '校验通过，正在写入启动入口…'
if [[ -n "$core_data_dir" ]]; then
  old_core_tmp=$(mktemp "$data_dir/.core-data-dir.XXXXXX")
  printf '%s\n' "$core_data_dir" > "$old_core_tmp"
  chmod 0600 "$old_core_tmp"
  mv -f "$old_core_tmp" "$data_dir/core-data-dir.txt"
fi
if ((device_name_set)); then
  device_name_tmp=$(mktemp "$data_dir/.device-name.XXXXXX")
  printf '%s\n' "$device_name" > "$device_name_tmp"
  chmod 0600 "$device_name_tmp"
  mv -f "$device_name_tmp" "$data_dir/device-name.txt"
fi
runner_tmp=$(mktemp "$data_dir/.run.XXXXXX")
cat > "$runner_tmp" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
IFS= read -r version < "$root/installed-version.txt"
[[ "$version" =~ ^v(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})$ ]] || { printf '安装版本文件无效\n' >&2; exit 1; }
args=(--data-dir "$root" --license-file "$root/secrets/license.key")
for setting in core-version device-type core-address login-address dashboard-address; do
  if [[ -f "$root/${setting}.txt" ]]; then
    IFS= read -r value < "$root/${setting}.txt"
    args+=("--${setting}" "$value")
  fi
done
if [[ -f "$root/core-data-dir.txt" ]]; then
  IFS= read -r old_core < "$root/core-data-dir.txt"
  args+=(--core-data-dir "$old_core")
fi
if [[ -f "$root/device-name.txt" ]]; then
  IFS= read -r device_name < "$root/device-name.txt"
  [[ -n "$device_name" ]] && args+=(--device-name "$device_name")
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
write_setting 'core-version.txt' "$core_version"
write_setting 'device-type.txt' "$device_type"
write_setting 'core-address.txt' "$core_address"
write_setting 'login-address.txt' "$login_address"
write_setting 'dashboard-address.txt' "$dashboard_address"
version_tmp=$(mktemp "$data_dir/.installed-version.XXXXXX")
printf '%s\n' "$version" > "$version_tmp"
chmod 0600 "$version_tmp"
mv -f "$version_tmp" "$data_dir/installed-version.txt"
printf '安装完成：we-clay %s\n运行入口：%s/run.sh\n控制台：http://%s\n' "$version" "$data_dir" "$dashboard_address"
printf '控制台管理员可在“微信连接”中查看二维码、登录状态并启停本安装管理的 core。\n'
if [[ "$login_address" != '-' ]]; then
  printf 'core 原登录页（兼容/应急）：http://%s\n' "$login_address"
fi
if ((existing_install)); then
  if ((restart)); then
    printf '正在启动新版本；core 会使用原账号目录恢复连接。\n'
  elif ((need_stop)); then
    printf '旧独立 core 已停止；请运行 %s/run.sh 启动新配置。\n' "$data_dir"
  else
    printf '检测到升级：请先停止旧 gateway，再运行 %s/run.sh；独立 core 会保持微信登录。\n' "$data_dir"
  fi
  run_after=0
fi
if ((run_after || restart)); then
  exec "$data_dir/run.sh"
fi
