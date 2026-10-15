# 发布时由 Action 将版本占位符替换为当前不可变 tag，并上传到私有 OSS。
param(
    [string]$KeyFile,
    [string]$DataDir = (Join-Path $env:LOCALAPPDATA 'we-clay'),
    [string]$CoreDataDir,
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

if ($version -notmatch '^v(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})$') { Fail '安装脚本尚未经过正式发布构建' }
if (-not [IO.Path]::IsPathRooted($DataDir)) { Fail 'DataDir 必须是绝对路径' }
if ($CoreDataDir -and ((-not [IO.Path]::IsPathRooted($CoreDataDir)) -or (-not (Test-Path -LiteralPath $CoreDataDir -PathType Container)))) {
    Fail 'CoreDataDir 必须指向已存在的绝对目录，避免误建新设备'
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
$assetName = "we-clay_${version}_windows_${goarch}.exe"
$uri = $null
if (-not [Uri]::TryCreate($assetUrl, [UriKind]::Absolute, [ref]$uri) -or
    $uri.Scheme -cne 'https' -or $uri.Host -cne $bucketHost -or
    $uri.AbsolutePath -cne "/we-clay/$version/$assetName" -or
    -not $uri.Query -or $assetUrl -match '[\x00-\x20"''\\]') {
    Fail '授权服务返回了非预期的 OSS 下载地址'
}
$temporary = Join-Path $binDir ('.we-clay.' + [Guid]::NewGuid().ToString('N'))
try {
    try {
        Invoke-WebRequest -Uri $assetUrl -OutFile $temporary -UseBasicParsing -MaximumRedirection 0 | Out-Null
    } catch { Fail '二进制下载失败；短时地址可能已过期，请重新运行安装脚本' }
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

if ($CoreDataDir) { Write-AtomicText (Join-Path $DataDir 'core-data-dir.txt') "$CoreDataDir`n" }
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
if (-not $NoRun -and -not $existingInstall) { & (Join-Path $DataDir 'run.ps1') }
