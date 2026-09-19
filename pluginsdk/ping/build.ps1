# ping 插件构建脚本 (Windows PowerShell)
# 用法: powershell -ExecutionPolicy Bypass -File pluginsdk\ping\build.ps1
# 产物: 1) %APPDATA%\AceShell\plugins\ping\        (开发安装, 立即生效)
#       2) internal\services\pluginbundle\ping\    (捆绑载荷, 随主程序发版嵌入)
#       3) plugins\aceshell-ping-windows-amd64.zip (release 资产, 供 GitHub 安装器)

$ErrorActionPreference = "Stop"
$scriptDir = if ($PSScriptRoot) { $PSScriptRoot } else { Split-Path -Parent $MyInvocation.MyCommand.Path }
$pluginDir = $scriptDir

Write-Host "== 1/3 构建插件前端 =="
Push-Location (Join-Path $pluginDir "frontend")
if (-not (Test-Path node_modules)) { npm install }
npm run build
Pop-Location

Write-Host "== 2/3 构建插件二进制并部署 =="
$exeName = "ping.exe"
foreach ($dest in @(
  (Join-Path $env:APPDATA "AceShell\plugins\ping"),
  (Join-Path $PSScriptRoot "..\..\internal\services\pluginbundle\ping")
)) {
  New-Item -ItemType Directory -Force -Path $dest | Out-Null
  Push-Location $pluginDir
  go build -trimpath -ldflags "-w -s" -o (Join-Path $dest $exeName) .
  Pop-Location
  Copy-Item (Join-Path $pluginDir "plugin.json") $dest -Force
  Copy-Item (Join-Path $pluginDir "dist") $dest -Recurse -Force
  Write-Host "  -> $dest"
}

Write-Host "== 3/3 打包 release zip =="
$pluginsRoot = Join-Path $env:APPDATA "AceShell\plugins"
$outZip = Join-Path $pluginsRoot "aceshell-ping-windows-amd64.zip"
if (Test-Path $outZip) { Remove-Item $outZip }
Compress-Archive -Path (Join-Path $pluginsRoot "ping\*") -DestinationPath $outZip
Write-Host "zip: $outZip"
