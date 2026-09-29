$ErrorActionPreference = "Stop"
$PSNativeCommandUseErrorActionPreference = $false

# 复制到项目 scripts/build-version.ps1 后, 通常只需要改 TagPrefix.
# 不要改后面的 tag / dirty 算法.
$TagPrefix = "v"

$root = Split-Path -Parent $PSScriptRoot
Push-Location -LiteralPath $root
try {

function Get-GitOutput {
    param(
        [Parameter(Mandatory = $true)]
        [string[]]$GitArgs
    )

    $output = & git @GitArgs 2>$null
    if ($LASTEXITCODE -ne 0) {
        return $null
    }

    $text = (($output | Out-String) -replace "`r", "").Trim()
    if ([string]::IsNullOrWhiteSpace($text)) {
        return $null
    }

    return $text
}

function Select-VersionTag {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Tags
    )

    $lines = $Tags -split "`n" | ForEach-Object { $_.Trim() } | Where-Object { $_ -ne "" }
    if ($TagPrefix) {
        $prefixed = $lines | Where-Object { $_.StartsWith($TagPrefix) } | Select-Object -First 1
        if ($prefixed) {
            return $prefixed
        }
    }

    return $lines | Select-Object -First 1
}

# 仓库一个版本 tag 都没有时的兜底版本号, 由调用方提供.
# 各语言模块给出该生态的结构化 metadata 读取命令, 在 just dist 或 CI 里先算出包版本,
# 再用 PROJECT_PACKAGE_VERSION 传进来.
function Read-PackageVersion {
    if ($env:PROJECT_PACKAGE_VERSION) {
        return $env:PROJECT_PACKAGE_VERSION
    }

    throw "仓库里没有任何版本 tag, 且未提供 PROJECT_PACKAGE_VERSION; 请先打一个版本 tag, 或按语言模块给出的命令读出包版本后传入该变量"
}

function Get-LatestDescribedTag {
    if ($TagPrefix) {
        return Get-GitOutput -GitArgs @("describe", "--tags", "--abbrev=0", "--match", "$TagPrefix*", "HEAD")
    }
    return Get-GitOutput -GitArgs @("describe", "--tags", "--abbrev=0", "HEAD")
}

function Strip-TagPrefix {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Display
    )

    if ($TagPrefix -and $Display.StartsWith($TagPrefix)) {
        return $Display.Substring($TagPrefix.Length)
    }
    return $Display
}

$exactTag = $null
$tags = Get-GitOutput -GitArgs @("tag", "--points-at", "HEAD")
if ($tags) {
    $exactTag = Select-VersionTag -Tags $tags
}

if ($exactTag) {
    $tag = $exactTag
} else {
    $described = Get-LatestDescribedTag
    if ($described) {
        $tag = $described
    } else {
        $tag = "$TagPrefix$(Read-PackageVersion)"
    }
}

$commit = Get-GitOutput -GitArgs @("rev-parse", "--short=7", "HEAD")
$dirty = $false
if ($commit) {
    & git diff-index --quiet HEAD -- | Out-Null
    if ($LASTEXITCODE -eq 1) {
        $dirty = $true
    }
}

if (-not $commit) {
    $display = $tag
} elseif ($dirty) {
    $display = "$tag^$commit"
} elseif ($exactTag) {
    $display = $tag
} else {
    $display = "$tag-$commit"
}

Write-Output (Strip-TagPrefix -Display $display)
} finally {
    Pop-Location
}
