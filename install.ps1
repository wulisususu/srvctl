# 把二进制装到 %LOCALAPPDATA%\srvctl 并加入用户 PATH。
#
#   .\install.ps1
#
# 不需要管理员权限：只写用户目录，只改 HKCU\Environment 下的 PATH，
# 不碰系统 PATH。

$ErrorActionPreference = 'Stop'

# 允许两种布局：仓库根目录（找 dist\）或直接对着 dist\ 目录运行
$src = Join-Path $PSScriptRoot 'dist'
if (-not (Test-Path (Join-Path $src 'srvctl.exe'))) {
    if (Test-Path (Join-Path $PSScriptRoot 'srvctl.exe')) {
        $src = $PSScriptRoot
    }
}

$dest = Join-Path $env:LOCALAPPDATA 'srvctl'
New-Item -ItemType Directory -Force -Path $dest | Out-Null

$files = @('srvctl.exe', 'srvctl-gui.exe', 'srvctl-ssidtest.exe')
$copied = 0
foreach ($f in $files) {
    $p = Join-Path $src $f
    if (Test-Path $p) {
        Copy-Item $p $dest -Force
        Write-Host "  已复制 $f"
        $copied++
    }
}
if ($copied -eq 0) {
    throw "在 $src 里没找到任何 srvctl 二进制。请先运行 .\build.ps1"
}

# 加入用户 PATH。
# 注意读的是 User 作用域的 PATH，不是 %PATH% —— 后者会把系统 PATH
# 也拼进来，导致用户 PATH 被撑到 1024 字符上限后截断。
$userPath = [Environment]::GetEnvironmentVariable('PATH', 'User')
if ($userPath -notlike "*$dest*") {
    $sep = if ([string]::IsNullOrEmpty($userPath)) { '' } else { ';' }
    [Environment]::SetEnvironmentVariable('PATH', "$userPath$sep$dest", 'User')
    Write-Host ''
    Write-Host "已把 $dest 加入用户 PATH —— 需要重开终端才生效" -ForegroundColor Yellow
}
else {
    Write-Host ''
    Write-Host "PATH 中已包含 $dest"
}

Write-Host ''
Write-Host "安装完成: $dest" -ForegroundColor Green
Write-Host ''
Write-Host '下一步：'
Write-Host '  1. 双击 srvctl-gui.exe 打开界面（或运行 srvctl gui）'
Write-Host '  2. 首次会让你设置主密码'
Write-Host '  3. 想让 AI 用 CLI：先运行 srvctl login'
Write-Host ''
Write-Host '验证安装：新开一个 PowerShell，运行 srvctl version'
