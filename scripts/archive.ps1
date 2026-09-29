# 语言无关的归档 helper, 复制到项目 scripts/archive.ps1. 规则与 archive.sh 完全一致:
#   PROJECT-VERSION-PLATFORM-ARCH[-VARIANT].EXT
# 平台无关的托管产物设 PLATFORM_INDEPENDENT=1 时命名为 PROJECT-VERSION[-VARIANT].EXT.
#
# 必需的环境变量: PROJECT_NAME, PROJECT_BUILD_VERSION
# 可选的环境变量: TARGET_PLATFORM, TARGET_ARCH, VARIANT, PLATFORM_INDEPENDENT, ARCHIVE_EXT, DIST_DIR
#
# 用法:
#   $env:PROJECT_NAME = "dida"; $env:PROJECT_BUILD_VERSION = "v0.1.2"
#   ./scripts/archive.ps1 -Staging build/stage
# 需要 PowerShell 7 及以上.

# param 必须是脚本的第一条语句, 所以放在前面的注释之后.
param(
    [Parameter(Mandatory = $true, Position = 0)]
    [string]$Staging,

    [Parameter(Position = 1, ValueFromRemainingArguments = $true)]
    [string[]]$Artifact
)

$ErrorActionPreference = "Stop"
$PSNativeCommandUseErrorActionPreference = $false

function Get-TargetPlatform {
    if ($env:TARGET_PLATFORM) { return $env:TARGET_PLATFORM }
    if ($IsWindows) { return "windows" }
    if ($IsMacOS) { return "macos" }
    if ($IsLinux) { return "linux" }
    throw "无法识别平台, 请显式设置 TARGET_PLATFORM"
}

function Get-TargetArch {
    if ($env:TARGET_ARCH) { return $env:TARGET_ARCH }
    switch ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()) {
        "X64" { return "x86_64" }
        "Arm64" { return "aarch64" }
        default { throw "无法识别架构, 请显式设置 TARGET_ARCH" }
    }
}

$projectName = $env:PROJECT_NAME
if (-not $projectName) { throw "缺少 PROJECT_NAME, 无法拼接产物名" }

$version = $env:PROJECT_BUILD_VERSION
if (-not $version) { throw "缺少 PROJECT_BUILD_VERSION, 无法拼接产物名" }

if (-not (Test-Path -LiteralPath $Staging -PathType Container)) {
    throw "staging 目录不存在: $Staging"
}

$variantSuffix = ""
if ($env:VARIANT) { $variantSuffix = "-$($env:VARIANT)" }

$platformSuffix = ""
if ($env:PLATFORM_INDEPENDENT -eq "1") {
    $ext = if ($env:ARCHIVE_EXT) { $env:ARCHIVE_EXT } else { "zip" }
} else {
    $platform = Get-TargetPlatform
    $arch = Get-TargetArch
    $platformSuffix = "-$platform-$arch"
    $defaultExt = if ($platform -eq "windows") { "zip" } else { "tar.gz" }
    $ext = if ($env:ARCHIVE_EXT) { $env:ARCHIVE_EXT } else { $defaultExt }
}

$distDir = if ($env:DIST_DIR) { $env:DIST_DIR } else { "dist" }
New-Item -ItemType Directory -Force -Path $distDir | Out-Null
$archive = Join-Path $distDir "$projectName-$version$platformSuffix${variantSuffix}.${ext}"

$artifacts = @()
if ($Artifact -and $Artifact.Count -gt 0) {
    foreach ($name in $Artifact) {
        $path = Join-Path $Staging $name
        if (-not (Test-Path -LiteralPath $path)) {
            throw "staging 目录缺少产物: $path"
        }
        $artifacts += $name
    }
} else {
    $staged = Get-ChildItem -LiteralPath $Staging -Force
    if (-not $staged -or $staged.Count -eq 0) {
        throw "staging 目录为空, 没有可打包的内容: $Staging"
    }
    $artifacts = $staged | ForEach-Object { $_.Name }
}

if (Test-Path -LiteralPath $archive) { Remove-Item -LiteralPath $archive -Force }

switch ($ext) {
    { $_ -in @("tar.gz", "tgz") } {
        & tar -czf $archive -C $Staging @artifacts
        if ($LASTEXITCODE -ne 0) { throw "tar 打包失败: $archive" }
    }
    "zip" {
        Compress-Archive -Path ($artifacts | ForEach-Object { Join-Path $Staging $_ }) -DestinationPath $archive
    }
    default { throw "不支持的扩展名: $ext" }
}

Write-Output "已生成 $archive"
