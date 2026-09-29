# just 使用说明: just --list

[private]
default:
    @just --list

# 编译到 bin/mosi-asr, 版本号显示 dev-build
#
# 开发构建关掉 VCS 信息写入, 否则每次 commit 都会改变链接结果, 让增量构建缓存失效.
build:
    go build -buildvcs=false -o bin/mosi-asr ./cmd/mosi-asr

# 生成当前平台的发布产物, 版本号自动带上 commit 短 hash
[macos]
dist:
    PROJECT_BUILD_VERSION="v$(bash scripts/build-version.sh)" bash scripts/dist.sh

# 生成当前平台的发布产物, 版本号自动带上 commit 短 hash
[linux]
dist:
    PROJECT_BUILD_VERSION="v$(bash scripts/build-version.sh)" bash scripts/dist.sh

# 生成当前平台的发布产物, 版本号自动带上 commit 短 hash
[windows]
[script('powershell.exe', '-NoProfile', '-ExecutionPolicy', 'Bypass', '-File')]
dist:
    $ErrorActionPreference = 'Stop'
    $env:PROJECT_BUILD_VERSION = "v$(& 'scripts/build-version.ps1' | Out-String).Trim()"
    & 'scripts/dist.ps1'
    if ($LASTEXITCODE) { exit $LASTEXITCODE }

# 当前构建应当显示的版本号
version:
    @bash scripts/build-version.sh

# 生成配置文件到 ~/.config/mosi-asr/config.toml
init:
    go run ./cmd/mosi-asr config init

# 直接运行: just run 录音.m4a -d
run *args:
    go run ./cmd/mosi-asr {{args}}

# 安装到 GOPATH/bin
install:
    go install ./cmd/mosi-asr

# 运行全部测试
test:
    go test ./... -count=1

# 查看当前生效的配置
show:
    go run ./cmd/mosi-asr config show

# 格式检查与静态检查, 只报告不修改
check:
    #!/usr/bin/env bash
    set -euo pipefail
    unformatted="$(gofmt -l .)"
    if [[ -n "$unformatted" ]]; then
        echo "以下文件未格式化:"
        echo "$unformatted"
        exit 1
    fi
    go vet ./...

# 自动格式化
fmt:
    gofmt -w .

# 整理依赖
tidy:
    go mod tidy

# 启动本地模拟服务 (默认 18080 端口), 不产生真实调用
mock port="18080":
    python3 scripts/mock-server.py {{port}}

# 用模拟服务跑一遍命令行冒烟测试
smoke port="18080":
    #!/usr/bin/env bash
    set -euo pipefail
    python3 scripts/mock-server.py {{port}} &
    mock_pid=$!
    trap 'kill $mock_pid 2>/dev/null || true' EXIT
    sleep 1
    workdir=$(mktemp -d)
    printf 'version = 1\n\n[api]\nbase_url = "http://127.0.0.1:{{port}}"\napi_key = "smoke-key"\n\n[log]\nlevel = "error"\n' > "$workdir/config.toml"
    printf 'fake audio\n' > "$workdir/测试.mp3"
    go run ./cmd/mosi-asr --config "$workdir/config.toml" -d -f srt "$workdir/测试.mp3"
    echo "---- 结果 ----"
    cat "$workdir/测试.srt"
