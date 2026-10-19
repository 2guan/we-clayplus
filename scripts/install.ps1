# 发布时由 Action 将版本占位符替换为当前不可变 tag，并上传到私有 OSS。
param(
    [string]$KeyFile,
    [string]$DataDir = (Join-Path $env:LOCALAPPDATA 'we-clay'),
    [string]$CoreDataDir,
    [string]$DeviceName,
    [switch]$NoRun
)

$ErrorActionPreference = 'Stop'
$version = '@WE_CLAY_VERSION@'
$authUrl = 'https://nww.hihfy.com/we-clay-auth/v1/download'
$bucketHost = 'weclay.oss-cn-beijing.aliyuncs.com'
$utf8 = New-Object System.Text.UTF8Encoding($false)

function Fail([string]$message) { throw "安装失败：$message" }
function Move-Atomic([string]$source, [string]$destination) {
    if (Test-Path -LiteralPath $destination) {
        [IO.File]::Replace($source, $destination, $null)
    } else {
        [IO.File]::Move($source, $destination)
    }
}
function Write-AtomicText([string]$path, [string]$value) {
    $temporary = "$path.tmp.$([Guid]::NewGuid().ToString('N'))"
    try {
        [IO.File]::WriteAllText($temporary, $value, $script:utf8)
        Move-Atomic $temporary $path
    } finally {
        if (Test-Path -LiteralPath $temporary) { Remove-Item -LiteralPath $temporary -Force }
    }
}
function Set-PrivateDirectoryAcl([string]$path) {
    # Restrict inheritance before creating any temporary Key file. The install
    # directory itself may be shared, but secrets/ must not be.
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent().Name
    $acl = Get-Acl -LiteralPath $path
    $acl.SetAccessRuleProtection($true, $false)
    foreach ($rule in @($acl.Access)) { [void]$acl.RemoveAccessRuleAll($rule) }
    $rule = [Security.AccessControl.FileSystemAccessRule]::new(
        $identity,
        [Security.AccessControl.FileSystemRights]::FullControl,
        ([Security.AccessControl.InheritanceFlags]::ContainerInherit -bor [Security.AccessControl.InheritanceFlags]::ObjectInherit),
        [Security.AccessControl.PropagationFlags]::None,
        [Security.AccessControl.AccessControlType]::Allow)
    $acl.AddAccessRule($rule)
    Set-Acl -LiteralPath $path -AclObject $acl
}
function Write-PrivateFile([string]$path, [string]$value) {
    $temporary = "$path.tmp.$([Guid]::NewGuid().ToString('N'))"
    [IO.File]::WriteAllText($temporary, $value, $script:utf8)
    try {
        $identity = [Security.Principal.WindowsIdentity]::GetCurrent().Name
        $acl = Get-Acl -LiteralPath $temporary
        $acl.SetAccessRuleProtection($true, $false)
        foreach ($rule in @($acl.Access)) { [void]$acl.RemoveAccessRuleAll($rule) }
        $rule = New-Object Security.AccessControl.FileSystemAccessRule($identity, 'FullControl', 'Allow')
        $acl.AddAccessRule($rule)
        Set-Acl -LiteralPath $temporary -AclObject $acl
        if (Test-Path -LiteralPath $path) { Set-Acl -LiteralPath $path -AclObject $acl }
        Move-Atomic $temporary $path
        Set-Acl -LiteralPath $path -AclObject $acl
    } finally {
        if (Test-Path -LiteralPath $temporary) { Remove-Item -LiteralPath $temporary -Force }
    }
}
function Download-WithProgress([string]$url, [string]$path, [long]$expectedBytes) {
    Add-Type -AssemblyName System.Net.Http
    $handler = New-Object System.Net.Http.HttpClientHandler
    $handler.AllowAutoRedirect = $false
    $client = [System.Net.Http.HttpClient]::new($handler)
    $client.Timeout = [TimeSpan]::FromMinutes(10)
    $request = [System.Net.Http.HttpRequestMessage]::new([System.Net.Http.HttpMethod]::Get, $url)
    $response = $null
    $source = $null
    $target = $null
    $timeout = [System.Threading.CancellationTokenSource]::new()
    $timeout.CancelAfter([TimeSpan]::FromMinutes(10))
    $downloaded = [long]0
    $nextReport = 10
    $watch = [Diagnostics.Stopwatch]::StartNew()
    try {
        $responseTask = $client.SendAsync($request, [System.Net.Http.HttpCompletionOption]::ResponseHeadersRead, $timeout.Token)
        $remainingMs = [int][Math]::Ceiling(600000 - $watch.Elapsed.TotalMilliseconds)
        if ($remainingMs -le 0 -or -not $responseTask.Wait($remainingMs)) {
            $timeout.Cancel()
            Fail '二进制下载超时；请重新运行安装脚本'
        }
        $response = $responseTask.GetAwaiter().GetResult()
        if (-not $response.IsSuccessStatusCode) { Fail '二进制下载失败；请重新运行安装脚本' }
        if ($response.Content.Headers.ContentLength -and $response.Content.Headers.ContentLength -ne $expectedBytes) {
            Fail '二进制大小与授权服务提供的清单不一致'
        }
        $streamTask = $response.Content.ReadAsStreamAsync()
        $remainingMs = [int][Math]::Ceiling(600000 - $watch.Elapsed.TotalMilliseconds)
        if ($remainingMs -le 0 -or -not $streamTask.Wait($remainingMs)) {
            $timeout.Cancel()
            Fail '二进制下载超时；请重新运行安装脚本'
        }
        $source = $streamTask.GetAwaiter().GetResult()
        $target = [IO.File]::Open($path, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::None)
        $buffer = New-Object byte[] (1024 * 1024)
        Write-Progress -Activity '下载 we-clay' -Status '0%' -PercentComplete 0
        Write-Host ('下载进度：0%（0 / {0:N1} MiB）' -f ($expectedBytes / 1MB))
        while ($true) {
            $remainingMs = [int][Math]::Ceiling(600000 - $watch.Elapsed.TotalMilliseconds)
            if ($remainingMs -le 0) { Fail '二进制下载超时；请重新运行安装脚本' }
            $readTask = $source.ReadAsync($buffer, 0, $buffer.Length, $timeout.Token)
            if (-not $readTask.Wait($remainingMs)) {
                $timeout.Cancel()
                Fail '二进制下载超时；请重新运行安装脚本'
            }
            $count = $readTask.GetAwaiter().GetResult()
            if ($count -le 0) { break }
            $downloaded += $count
            if ($downloaded -gt $expectedBytes) { Fail '二进制大小超过授权服务提供的清单' }
            $target.Write($buffer, 0, $count)
            $percent = [int][Math]::Floor(100.0 * $downloaded / $expectedBytes)
            Write-Progress -Activity '下载 we-clay' -Status "$percent%" -PercentComplete $percent
            if ($percent -ge $nextReport) {
                Write-Host ('下载进度：{0}%（{1:N1} / {2:N1} MiB）' -f $percent, ($downloaded / 1MB), ($expectedBytes / 1MB))
                while ($nextReport -le $percent) { $nextReport += 10 }
            }
        }
        $target.Flush()
        if ($downloaded -ne $expectedBytes) { Fail '二进制大小校验失败' }
        Write-Host ('下载完成：{0:N1} MiB，耗时 {1:N0} 秒' -f ($downloaded / 1MB), $watch.Elapsed.TotalSeconds)
    } finally {
        Write-Progress -Activity '下载 we-clay' -Completed
        if ($target) { $target.Dispose() }
        if ($source) { $source.Dispose() }
        if ($response) { $response.Dispose() }
        $request.Dispose()
        $client.Dispose()
        $handler.Dispose()
        $timeout.Dispose()
    }
}

Write-Host '[1/5] 检查系统和安装目录...'
if ($version -notmatch '^v(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})$') { Fail '安装脚本尚未经过正式发布构建' }
if (-not [IO.Path]::IsPathRooted($DataDir)) { Fail 'DataDir 必须是绝对路径' }
if ($CoreDataDir -and ((-not [IO.Path]::IsPathRooted($CoreDataDir)) -or (-not (Test-Path -LiteralPath $CoreDataDir -PathType Container)))) {
    Fail 'CoreDataDir 必须指向已存在的绝对目录，避免误建新设备'
}
if ($PSBoundParameters.ContainsKey('DeviceName') -and
    ([string]::IsNullOrWhiteSpace($DeviceName) -or $DeviceName -match '[\x00\r\n]')) {
    Fail 'DeviceName 不能为空或包含换行和 NUL 字符'
}
$architecture = [Environment]::GetEnvironmentVariable('PROCESSOR_ARCHITEW6432')
if (-not $architecture) { $architecture = [Environment]::GetEnvironmentVariable('PROCESSOR_ARCHITECTURE') }
switch ($architecture.ToUpperInvariant()) {
    'AMD64' { $goarch = 'amd64' }
    'ARM64' { $goarch = 'arm64' }
    default { Fail '当前 CPU 架构尚无发布文件' }
}

$secretsDir = Join-Path $DataDir 'secrets'
$binDir = Join-Path $DataDir 'bin'
New-Item -ItemType Directory -Path $secretsDir, $binDir -Force | Out-Null
Set-PrivateDirectoryAcl $secretsDir
$licenseFile = Join-Path $secretsDir 'license.key'
$existingInstall = Test-Path -LiteralPath (Join-Path $DataDir 'installed-version.txt')
Write-Host '[2/5] 读取授权并申请下载地址...'
if ($KeyFile) {
    if (-not (Test-Path -LiteralPath $KeyFile -PathType Leaf)) { Fail '授权文件不存在' }
    $key = [IO.File]::ReadAllText($KeyFile).Trim()
} elseif (Test-Path -LiteralPath $licenseFile -PathType Leaf) {
    $key = [IO.File]::ReadAllText($licenseFile).Trim()
} else {
    $secure = Read-Host '请输入授权 Key（不回显）' -AsSecureString
    $bstr = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secure)
    try { $key = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($bstr) }
    finally { [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($bstr) }
}
if ($key -cnotmatch '^wca_[0-9a-f]{64}$') { Fail '授权 Key 格式无效' }

$request = @{product='we-clay'; version=$version; goos='windows'; goarch=$goarch; asset='bundle'} | ConvertTo-Json -Compress
$headers = @{Authorization="Bearer $key"; Accept='text/plain'}
try {
    $response = Invoke-WebRequest -Uri $authUrl -Method Post -Headers $headers -ContentType 'application/json' -Body $request -UseBasicParsing -MaximumRedirection 0
} catch {
    Fail '授权服务拒绝下载或暂时不可用；请检查 Key、有效期和网络'
} finally {
    $headers = $null
}
$lines = [regex]::Split($response.Content.TrimEnd("`r", "`n"), '\r?\n')
if ($lines.Count -ne 4) { Fail '授权服务返回格式错误' }
$resolvedVersion, $assetUrl, $expectedHash, $expectedSize = $lines
if ($resolvedVersion -cne $version) { Fail '下载版本与安装脚本不一致' }
if ($expectedHash -cnotmatch '^[0-9a-f]{64}$' -or $expectedSize -cnotmatch '^[0-9]+$') { Fail '下载摘要或大小无效' }
$expectedBytes = [long]0
if (-not [long]::TryParse($expectedSize, [ref]$expectedBytes) -or $expectedBytes -le 0) { Fail '下载文件大小无效' }
$assetName = "we-clay_${version}_windows_${goarch}.exe"
$uri = $null
if (-not [Uri]::TryCreate($assetUrl, [UriKind]::Absolute, [ref]$uri) -or
    $uri.Scheme -cne 'https' -or $uri.Host -cne $bucketHost -or
    $uri.AbsolutePath -cne "/we-clay/$version/$assetName" -or
    -not $uri.Query -or $assetUrl -match '[\x00-\x20"''\\]') {
    Fail '授权服务返回了非预期的 OSS 下载地址'
}
$temporary = Join-Path $binDir ('.we-clay.' + [Guid]::NewGuid().ToString('N'))
Write-Host "[3/5] 下载 we-clay $version（windows/$goarch）..."
try {
    try {
        Download-WithProgress $assetUrl $temporary $expectedBytes
    } catch { Fail '二进制下载失败或超时；请检查网络并重新运行安装脚本' }
    Write-Host '[4/5] 校验文件大小和 SHA-256...'
    $file = Get-Item -LiteralPath $temporary
    if ($file.Length.ToString() -cne $expectedSize) { Fail '二进制大小校验失败' }
    $actualHash = (Get-FileHash -LiteralPath $temporary -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actualHash -cne $expectedHash) { Fail '二进制 SHA-256 校验失败' }
    $destination = Join-Path $binDir "we-clay_$version.exe"
    if (Test-Path -LiteralPath $destination) {
        $installedHash = (Get-FileHash -LiteralPath $destination -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($installedHash -cne $expectedHash) { Fail '同版本本地二进制与已发布摘要不一致' }
    } else {
        Move-Item -LiteralPath $temporary -Destination $destination
    }
} finally {
    if (Test-Path -LiteralPath $temporary) { Remove-Item -LiteralPath $temporary -Force }
}

Write-Host '[5/5] 保存运行配置和启动入口...'
if ($CoreDataDir) { Write-AtomicText (Join-Path $DataDir 'core-data-dir.txt') "$CoreDataDir`n" }
if ($PSBoundParameters.ContainsKey('DeviceName')) {
    Write-AtomicText (Join-Path $DataDir 'device-name.txt') "$DeviceName`n"
}
$runner = @'
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSCommandPath
$version = [IO.File]::ReadAllText((Join-Path $root 'installed-version.txt')).Trim()
if ($version -notmatch '^v(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})$') { throw '安装版本文件无效' }
$arguments = @('--data-dir', $root, '--license-file', (Join-Path $root 'secrets\license.key'))
$oldCore = Join-Path $root 'core-data-dir.txt'
if (Test-Path -LiteralPath $oldCore) {
    $arguments += @('--core-data-dir', [IO.File]::ReadAllText($oldCore).Trim())
}
$deviceNameFile = Join-Path $root 'device-name.txt'
if (Test-Path -LiteralPath $deviceNameFile -PathType Leaf) {
    $deviceName = [IO.File]::ReadAllText($deviceNameFile).TrimEnd([char[]]@([char]13, [char]10))
    if ([string]::IsNullOrWhiteSpace($deviceName) -or $deviceName -match '[\x00\r\n]') { throw '设备名称配置无效' }
    $arguments += @('--device-name', $deviceName)
}
& (Join-Path $root "bin\we-clay_$version.exe") @arguments @args
exit $LASTEXITCODE
'@
Write-AtomicText (Join-Path $DataDir 'run.ps1') $runner
$runnerCmd = @'
@echo off
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0run.ps1" %*
exit /b %ERRORLEVEL%
'@
Write-AtomicText (Join-Path $DataDir 'run.cmd') $runnerCmd
if ($KeyFile -or -not (Test-Path -LiteralPath $licenseFile)) {
    Write-PrivateFile $licenseFile ($key + "`n")
}
$key = $null
Write-AtomicText (Join-Path $DataDir 'installed-version.txt') "$version`n"
Write-Host "安装完成：we-clay $version"
Write-Host "运行入口：$(Join-Path $DataDir 'run.cmd')"
Write-Host '控制台：http://127.0.0.1:8208'
if ($existingInstall) { Write-Host '检测到升级：请先停止旧 gateway，再运行 run.cmd；独立 core 会保持微信登录。' }
if ($NoRun) { Write-Host '已按 NoRun 跳过自动启动。' }
elseif (-not $existingInstall) {
    Write-Host '首次安装完成，正在启动 we-clay；首次运行还可能下载 core。'
    & (Join-Path $DataDir 'run.ps1')
}
