#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${SILIECO_DOWNLOAD_BASE_URL:-http://yilia.me:6688/cli}"

info() { printf '==> %s\n' "$*"; }
fail() { printf 'Error: %s\n' "$*" >&2; exit 1; }

case "$(uname -s)" in
  Darwin) os="darwin" ;;
  Linux) os="linux" ;;
  MINGW*|MSYS*|CYGWIN*)
    fail "Windows 请使用 PowerShell 安装命令：irm ${BASE_URL}/install.ps1 | iex"
    ;;
  *) fail "不支持的操作系统：$(uname -s)" ;;
esac

case "$(uname -m)" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) fail "不支持的处理器架构：$(uname -m)" ;;
esac

archive="silieco-${os}-${arch}.tar.gz"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

info "正在从 ${BASE_URL} 下载 Silieco CLI (${os}/${arch})..."
curl -fsSL "${BASE_URL}/${archive}" -o "${tmp_dir}/${archive}"
curl -fsSL "${BASE_URL}/checksums.txt" -o "${tmp_dir}/checksums.txt"

expected="$(awk -v file="$archive" '$2 == file { print $1 }' "${tmp_dir}/checksums.txt")"
[ -n "$expected" ] || fail "校验文件中没有 ${archive}"

if command -v shasum >/dev/null 2>&1; then
  actual="$(shasum -a 256 "${tmp_dir}/${archive}" | awk '{print $1}')"
elif command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "${tmp_dir}/${archive}" | awk '{print $1}')"
else
  fail "缺少 SHA-256 校验工具（shasum 或 sha256sum）"
fi
[ "$actual" = "$expected" ] || fail "下载文件校验失败"

tar -xzf "${tmp_dir}/${archive}" -C "$tmp_dir" silieco
chmod +x "${tmp_dir}/silieco"

bin_dir="${SILIECO_BIN_DIR:-/usr/local/bin}"
if [ -d "$bin_dir" ] && [ -w "$bin_dir" ]; then
  mv "${tmp_dir}/silieco" "${bin_dir}/silieco"
elif command -v sudo >/dev/null 2>&1; then
  sudo mkdir -p "$bin_dir"
  sudo mv "${tmp_dir}/silieco" "${bin_dir}/silieco"
else
  bin_dir="$HOME/.local/bin"
  mkdir -p "$bin_dir"
  mv "${tmp_dir}/silieco" "${bin_dir}/silieco"
  case ":$PATH:" in
    *":${bin_dir}:"*) ;;
    *)
      shell_rc="$HOME/.profile"
      [ -n "${ZSH_VERSION:-}" ] && shell_rc="$HOME/.zshrc"
      printf '\n# Added by Silieco CLI installer\nexport PATH="%s:$PATH"\n' "$bin_dir" >> "$shell_rc"
      export PATH="$bin_dir:$PATH"
      ;;
  esac
fi

info "Silieco CLI 已安装到 ${bin_dir}/silieco"
"${bin_dir}/silieco" version
printf '\n下一步：\n  silieco setup self-host --server-url http://yilia.me:6688 --app-url http://yilia.me:6688\n'
