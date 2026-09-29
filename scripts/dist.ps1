$ErrorActionPreference = "Stop"
$PSNativeCommandUseErrorActionPreference = $false

# 生成当前平台的发布产物.
# 需要同目录下的 scripts/archive.ps1 与 scripts/build-version.ps1, 规则与 dist.sh 一致.
$ProjectName = "mosi-asr"
$MainPackage = "./cmd/mosi-asr"
$BinaryName = "mosi-asr"
$SmokeArgs = @("--version")
# 归档里随二进制一起分发的说明文件.
$ExtraFiles = @("README.md")

$root = Split-Path -Parent $PSScriptRoot
Push-Location -LiteralPath $root
try {
    $platform = (go env GOOS)
    $arch = (go env GOARCH)
    switch ($platform) {
        "darwin" { $platform = "macos" }
        "linux" { $platform = "linux" }
        "windows" { $platform = "windows" }
    }
    switch ($arch) {
        "amd64" { $arch = "x86_64" }
        "arm64" { $arch = "aarch64" }
    }

    # 二进制内显示的版本跟随 tag 样式, 通常带 v 前缀.
    $version = $env:PROJECT_BUILD_VERSION
    if (-not $version) {
        $version = "v$(& 'scripts/build-version.ps1' | Out-String).Trim()"
    }

    # 空版本或只有 v 说明版本解析没成功, 这时打出来的包名会残缺, 直接失败.
    if (-not $version -or $version -eq "v") {
        throw "版本号为空: 请设置 PROJECT_BUILD_VERSION, 或确保仓库里已有版本 tag"
    }

    # 产物名里的版本段去掉 v 前缀, 与 PROJECT-VERSION-PLATFORM-ARCH 的命名示例保持一致.
    $archiveVersion = $version
    if ($archiveVersion.StartsWith("v")) {
        $archiveVersion = $archiveVersion.Substring(1)
    }

    Write-Host "构建 $ProjectName $version ($platform-$arch)"

    $binary = $BinaryName
    if ($platform -eq "windows") { $binary = "${BinaryName}.exe" }

    $staging = "dist/stage"
    if (Test-Path -LiteralPath $staging) { Remove-Item -LiteralPath $staging -Recurse -Force }
    New-Item -ItemType Directory -Force -Path $staging | Out-Null

    $module = (go list -m)
    $env:CGO_ENABLED = "0"
    go build -trimpath -ldflags "-s -w -X $module/internal/buildinfo.version=$version" -o "$staging/$binary" $MainPackage
    if ($LASTEXITCODE -ne 0) { throw "构建失败" }

    $reported = (& "$staging/$binary" @SmokeArgs | Out-String)
    if ($reported -notmatch [regex]::Escape($version)) {
        throw "版本号校验失败: 期望 $version, 实际输出 $reported"
    }
    Write-Host "版本号校验通过: $version"

    # 归档内容: 二进制本身, 加上随包分发的说明文件.
    $archived = @($binary)
    foreach ($extra in $ExtraFiles) {
        if (-not (Test-Path -LiteralPath $extra -PathType Leaf)) {
            throw "缺少要打包的说明文件: $extra"
        }
        Copy-Item -LiteralPath $extra -Destination "$staging/$extra" -Force
        $archived += $extra
    }

    $env:PROJECT_NAME = $ProjectName
    $env:PROJECT_BUILD_VERSION = $archiveVersion
    $env:TARGET_PLATFORM = $platform
    $env:TARGET_ARCH = $arch
    & 'scripts/archive.ps1' -Staging $staging @archived
    if ($LASTEXITCODE -ne 0) { throw "归档失败" }
} finally {
    Pop-Location
}
