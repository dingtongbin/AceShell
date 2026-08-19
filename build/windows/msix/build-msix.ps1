<#
AceShell MSIX 打包脚本(不签名,供微软商店提交,商店侧会自动重签)

Usage:
  powershell -ExecutionPolicy Bypass -File build\windows\msix\build-msix.ps1 -Version 0.1.0

要求:
  - bin/AceShell.exe 已构建(wails3 build)
  - makeappx.exe 位于 Windows SDK(自动查找)

产物:
  bin/AceShell-<Version>.msix
#>
param(
  [string]$Version = '0.1.0'
)

$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent (Split-Path -Parent (Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)))
$MsixDir = Join-Path $Root 'build\windows\msix'
$ExePath = Join-Path $Root 'bin\AceShell.exe'
$IconSource = Join-Path $Root 'build\appicon.png'
$OutDir = Join-Path $Root 'bin'

if (-not (Test-Path $ExePath)) {
  throw "未找到 $ExePath,请先执行 wails3 build"
}
if (-not (Test-Path $IconSource)) {
  throw "未找到图标源 $IconSource"
}

# ---------- 查找 makeappx ----------
$makeappx = Get-ChildItem 'C:\Program Files (x86)\Windows Kits\10\bin' -Recurse -Filter 'makeappx.exe' -ErrorAction SilentlyContinue |
  Where-Object { $_.FullName -match '\\x64\\' } | Select-Object -First 1
if (-not $makeappx) {
  $makeappx = Get-ChildItem 'C:\Program Files (x86)\Windows Kits\10\bin' -Recurse -Filter 'makeappx.exe' -ErrorAction SilentlyContinue | Select-Object -First 1
}
if (-not $makeappx) {
  throw '未找到 makeappx.exe,请安装 Windows SDK (https://developer.microsoft.com/windows/downloads/windows-sdk/)'
}

# ---------- 组装 payload ----------
$Payload = Join-Path $OutDir 'msix-payload'
if (Test-Path $Payload) { Remove-Item $Payload -Recurse -Force }
New-Item -ItemType Directory -Path $Payload | Out-Null
New-Item -ItemType Directory -Path (Join-Path $Payload 'Assets') | Out-Null

Copy-Item -LiteralPath $ExePath -Destination (Join-Path $Payload 'AceShell.exe')
Copy-Item -LiteralPath (Join-Path $MsixDir 'app_manifest.xml') -Destination (Join-Path $Payload 'AppxManifest.xml')

# 替换 manifest 中的版本号
$manifest = Join-Path $Payload 'AppxManifest.xml'
$content = Get-Content -LiteralPath $manifest -Raw -Encoding UTF8
$content = $content -replace '(?<![A-Za-z])Version="[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+"', "Version=`"$Version.0`""
Set-Content -LiteralPath $manifest -Value $content -Encoding UTF8 -NoNewline

# ---------- 生成 MSIX 图标(从 appicon.png 缩放) ----------
Add-Type -AssemblyName System.Drawing
$sizes = @{
  'StoreLogo.png'            = 300
  'Square44x44Logo.png'      = 44
  'Square150x150Logo.png'    = 150
  'Wide310x150Logo.png'      = 310
  'SplashScreen.png'         = 620
}
$src = [System.Drawing.Image]::FromFile($IconSource)
foreach ($name in $sizes.Keys) {
  $size = $sizes[$name]
  $w = $size
  $h = if ($name -eq 'Wide310x150Logo.png') { 150 } elseif ($name -eq 'SplashScreen.png') { 300 } else { $size }
  $bmp = New-Object System.Drawing.Bitmap($w, $h)
  $g = [System.Drawing.Graphics]::FromImage($bmp)
  $g.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
  $g.Clear([System.Drawing.Color]::Transparent)
  $g.DrawImage($src, 0, 0, $w, $h)
  $g.Dispose()
  $bmp.Save((Join-Path $Payload "Assets\$name"), [System.Drawing.Imaging.ImageFormat]::Png)
  $bmp.Dispose()
}
$src.Dispose()

# ---------- 打包 ----------
$OutMsix = Join-Path $OutDir "AceShell-$Version.msix"
if (Test-Path $OutMsix) { Remove-Item $OutMsix -Force }
& $makeappx.FullName pack /d $Payload /p $OutMsix /o
if ($LASTEXITCODE -ne 0) { throw 'makeappx pack 失败' }

Remove-Item $Payload -Recurse -Force
Write-Host "MSIX 已生成: $OutMsix (未签名,提交商店后由微软重签)"
