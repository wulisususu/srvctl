# 构建 srvctl 的全部二进制到 dist\
#
#   .\build.ps1                  只构建 Windows
#   .\build.ps1 -AllPlatforms    额外交叉编译 Linux / macOS
#
# 需要 Go 1.23+（https://go.dev/dl/）。首次运行需要联网拉依赖。

param(
    [switch]$AllPlatforms
)

$ErrorActionPreference = 'Stop'

# 静态链接，不依赖任何 C 运行库 —— 这是"单文件、零依赖"的前提
$env:CGO_ENABLED = '0'

$root = $PSScriptRoot
Push-Location $root

try {
    if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
        throw "没找到 go 命令。请先安装 Go: https://go.dev/dl/"
    }
    Write-Host "Go 版本: $(go version)" -ForegroundColor Cyan
    Write-Host ''

    New-Item -ItemType Directory -Force -Path dist | Out-Null
    $strip = '-s -w'   # 去掉符号表和调试信息，体积减半

    Write-Host '构建 srvctl.exe（控制台版，给命令行和 AI 用）...'
    go build -trimpath -ldflags $strip -o dist\srvctl.exe .\cmd\srvctl
    if ($LASTEXITCODE -ne 0) { throw 'srvctl.exe 构建失败' }

    Write-Host '构建 srvctl-gui.exe（无窗口版，双击开界面不弹黑框）...'
    go build -trimpath -ldflags "$strip -H=windowsgui" -o dist\srvctl-gui.exe .\cmd\srvctl-gui
    if ($LASTEXITCODE -ne 0) { throw 'srvctl-gui.exe 构建失败' }

    Write-Host '构建 srvctl-ssidtest.exe（SSID 探测验证工具）...'
    go build -trimpath -ldflags $strip -o dist\srvctl-ssidtest.exe .\tools\ssidtest
    if ($LASTEXITCODE -ne 0) { throw 'srvctl-ssidtest.exe 构建失败' }

    if ($AllPlatforms) {
        Write-Host ''
        foreach ($t in @(@('linux', 'amd64'), @('darwin', 'amd64'), @('darwin', 'arm64'))) {
            $env:GOOS = $t[0]; $env:GOARCH = $t[1]
            $out = "dist\srvctl-$($t[0])-$($t[1])"
            Write-Host "交叉编译 $out ..."
            go build -trimpath -ldflags $strip -o $out .\cmd\srvctl
            if ($LASTEXITCODE -ne 0) { throw "$out 构建失败" }
        }
        $env:GOOS = 'windows'; $env:GOARCH = 'amd64'
    }

    Write-Host ''
    Write-Host '构建完成：' -ForegroundColor Green
    Get-ChildItem dist | Select-Object Name, @{n = 'MB'; e = { [math]::Round($_.Length / 1MB, 2) } } | Format-Table -AutoSize

    Write-Host '下一步: .\install.ps1' -ForegroundColor Cyan
}
finally {
    Pop-Location
    $env:GOOS = 'windows'; $env:GOARCH = 'amd64'
}
