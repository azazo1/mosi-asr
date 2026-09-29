#!/usr/bin/env bash

# 生成当前平台的发布产物.
#
# 需要同目录下的 scripts/archive.sh 与 scripts/build-version.sh.
# 版本号来自 PROJECT_BUILD_VERSION 环境变量, 未设置时回退到 build-version.sh.
PROJECT_NAME="mosi-asr"
MAIN_PACKAGE="./cmd/mosi-asr"
BINARY_NAME="mosi-asr"
# 冒烟检查用的参数, 要求二进制能报出版本号.
SMOKE_ARGS=(--version)
# 归档里随二进制一起分发的说明文件.
EXTRA_FILES=(README.md)

set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

platform="$(go env GOOS)"
arch="$(go env GOARCH)"
case "$platform" in
  darwin) platform="macos" ;;
  linux) platform="linux" ;;
  windows) platform="windows" ;;
esac
case "$arch" in
  amd64) arch="x86_64" ;;
  arm64) arch="aarch64" ;;
esac

# 二进制内显示的版本跟随 tag 样式, 通常带 v 前缀.
version="${PROJECT_BUILD_VERSION:-}"
if [[ -z "$version" ]]; then
  version="v$(bash scripts/build-version.sh)"
fi

# 空版本或只有 v 说明版本解析没成功, 这时打出来的包名会残缺, 直接失败.
if [[ -z "$version" || "$version" == "v" ]]; then
  echo "版本号为空: 请设置 PROJECT_BUILD_VERSION, 或确保仓库里已有版本 tag" >&2
  exit 1
fi

# 产物名里的版本段去掉 v 前缀, 与 PROJECT-VERSION-PLATFORM-ARCH 的命名示例保持一致.
archive_version="${version#v}"

echo "构建 $PROJECT_NAME $version ($platform-$arch)"

binary="$BINARY_NAME"
if [[ "$platform" == "windows" ]]; then
  binary="$BINARY_NAME.exe"
fi

staging="dist/stage"
rm -rf "$staging"
mkdir -p "$staging"

# 注入路径必须是完整包路径, 所以这里用 go list -m 取当前模块名.
module="$(go list -m)"
CGO_ENABLED=0 go build \
  -trimpath \
  -ldflags "-s -w -X ${module}/internal/buildinfo.version=$version" \
  -o "$staging/$binary" \
  "$MAIN_PACKAGE"

reported="$("$staging/$binary" "${SMOKE_ARGS[@]}")"
if [[ "$reported" != *"$version"* ]]; then
  echo "版本号校验失败: 期望 $version, 实际输出 $reported" >&2
  exit 1
fi
echo "版本号校验通过: $version"

# 归档内容: 二进制本身, 加上随包分发的说明文件.
archived=("$binary")
for extra in "${EXTRA_FILES[@]}"; do
  if [[ ! -f "$extra" ]]; then
    echo "缺少要打包的说明文件: $extra" >&2
    exit 1
  fi
  cp "$extra" "$staging/$extra"
  archived+=("$extra")
done

PROJECT_NAME="$PROJECT_NAME" \
PROJECT_BUILD_VERSION="$archive_version" \
TARGET_PLATFORM="$platform" \
TARGET_ARCH="$arch" \
  bash scripts/archive.sh "$staging" "${archived[@]}"
