# just 使用说明: just --list

# 版本号, 会注入到二进制; 发版时改成对应 tag
version := "dev"

[private]
default:
    @just --list

# 编译到 bin/mosi-asr
build:
    go build -ldflags "-X main.version={{version}}" -o bin/mosi-asr ./cmd/mosi-asr

# 生成配置文件到 ~/.config/mosi-asr/config.toml
init:
    go run ./cmd/mosi-asr config init

# 直接运行: just run 录音.m4a -d
run *args:
    go run ./cmd/mosi-asr {{args}}

# 安装到 GOPATH/bin
install:
    go install -ldflags "-X main.version={{version}}" ./cmd/mosi-asr

# 运行全部测试
test:
    go test ./...

# 查看当前生效的配置
show:
    go run ./cmd/mosi-asr config show

# 格式检查与静态检查
check:
    gofmt -l .
    go vet ./...

# 自动格式化
fmt:
    gofmt -w .

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
