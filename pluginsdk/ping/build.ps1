# ping 插件构建脚本 (PowerShell, 三平台通用: Windows powershell / linux·macOS pwsh)
# 用法: powershell -NoProfile -ExecutionPolicy Bypass -File pluginsdk\ping\build.ps1   (Windows)
#       pwsh -NoProfile -File pluginsdk/ping/build.ps1                                 (linux/macOS)
# 产物: 1) internal/services/pluginbundle/ping/             (捆绑载荷, 随主程序嵌入, 必需)
#       2) 平台用户插件目录\ping\                            (开发安装, 立即生效)
#       3) pluginsdk/ping/aceshell-ping-<goos>-<goarch>.zip (release 资产, 供 GitHub 安装器)

$ErrorActionPreference = "Stop"
$scriptDir = if ($PSScriptRoot) { $PSScriptRoot } else { Split-Path -Parent $MyInvocation.MyCommand.Path }

# PS5.1 没有 $IsWindows, 用 .NET 判定(两种宿主行为一致)
$isWin = [System.Runtime.InteropServices.RuntimeInformation]::IsOSPlatform([System.Runtime.InteropServices.OSPlatform]::Windows)
$isMac = [System.Runtime.InteropServices.RuntimeInformation]::IsOSPlatform([System.Runtime.InteropServices.OSPlatform]::OSX)
$exeName = if ($isWin) { "ping.exe" } else { "ping" }
$goos = (& go env GOOS).Trim()
$goarch = (& go env GOARCH).Trim()
$bundleDir = Join-Path $scriptDir "..\..\internal\services\pluginbundle\ping"

# 与宿主 apppaths.go 的 PluginsDir 保持一致(开发安装; CI 上多余但无害)
function userPluginsRoot {
  if ($isWin) { return (Join-Path $env:APPDATA "AceShell\plugins") }
  if ($isMac) { return (Join-Path $HOME "Library/Application Support/AceShell/plugins") }
  $base = $env:XDG_CONFIG_HOME
  if (-not $base) { $base = Join-Path $HOME ".config" }
  return (Join-Path $base "aceshell/plugins")
}

function deploy([string]$dest) {
  New-Item -ItemType Directory -Force -Path $dest | Out-Null
  Remove-Item (Join-Path $dest "dist") -Recurse -Force -ErrorAction SilentlyContinue
  Push-Location $scriptDir
  go build -trimpath -ldflags "-w -s" -o (Join-Path $dest $exeName) .
  Pop-Location
  if ($LASTEXITCODE -ne 0) { throw "go build 失败: $dest" }
  Copy-Item (Join-Path $scriptDir "plugin.json") $dest -Force
  Copy-Item (Join-Path $scriptDir "dist") $dest -Recurse -Force
  # 文档钩子: plugin.json 的 docs 字段指向 docs/ 下的 md, 随载荷一并部署
  if (Test-Path (Join-Path $scriptDir "docs")) {
    Copy-Item (Join-Path $scriptDir "docs") $dest -Recurse -Force
  }
  Write-Host "  -> $dest"
}

Write-Host "== 1/3 构建插件前端 =="
Push-Location (Join-Path $scriptDir "frontend")
try {
  if (-not (Test-Path node_modules)) {
    npm install
    if ($LASTEXITCODE -ne 0) { throw "npm install 失败" }
  }
  npm run build
  if ($LASTEXITCODE -ne 0) { throw "npm run build 失败" }
} finally { Pop-Location }

Write-Host "== 2/3 构建插件二进制并部署 =="
deploy $bundleDir
deploy (Join-Path (userPluginsRoot) "ping")

Write-Host "== 3/3 打包 release zip =="
# 用 .NET ZipFile 而非 Compress-Archive: 后者在 Windows 上会用 '\' 作 zip 条目
# 分隔符, Go 安装器(unzipTo)在 linux/mac 解压时会得到带反斜杠的文件名。
$stage = Join-Path ([System.IO.Path]::GetTempPath()) ("aceshell-ping-pkg-" + [guid]::NewGuid().ToString("N").Substring(0, 8))
$outZip = Join-Path $scriptDir ("aceshell-ping-" + $goos + "-" + $goarch + ".zip")
try {
  New-Item -ItemType Directory -Force -Path $stage | Out-Null
  Copy-Item (Join-Path $bundleDir $exeName) $stage
  Copy-Item (Join-Path $scriptDir "plugin.json") $stage
  Copy-Item (Join-Path $scriptDir "dist") (Join-Path $stage "dist") -Recurse
  if (Test-Path (Join-Path $scriptDir "docs")) {
    Copy-Item (Join-Path $scriptDir "docs") (Join-Path $stage "docs") -Recurse
  }
  if (Test-Path $outZip) { Remove-Item $outZip -Force }
  try { Add-Type -AssemblyName System.IO.Compression.FileSystem } catch {}
  [System.IO.Compression.ZipFile]::CreateFromDirectory($stage, $outZip, [System.IO.Compression.CompressionLevel]::Optimal, $false)
  Write-Host "zip: $outZip"
} finally {
  Remove-Item $stage -Recurse -Force -ErrorAction SilentlyContinue
}
