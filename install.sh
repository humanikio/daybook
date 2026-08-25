#!/bin/sh
# daybook installer
#   curl -fsSL https://github.com/humanikio/daybook/releases/latest/download/install.sh | sh
#
# Downloads the right prebuilt binary for this OS/arch and installs it to
# ~/.local/bin. Does NOT run setup or install the service — `daybook init`
# does both, and it asks questions nobody should answer on your behalf.
set -eu

VERSION="${DAYBOOK_VERSION:-latest}"
REPO="${DAYBOOK_REPO:-humanikio/daybook}"

# User-level by default, unlike a system daemon. daybook reads YOUR transcripts
# and YOUR git identity and runs as you, so there is nothing to gain from a
# root-owned binary in /usr/local/bin and a sudo prompt to lose.
DEST="${DAYBOOK_BIN:-$HOME/.local/bin/daybook}"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "daybook: unsupported arch '$arch'" >&2; exit 1 ;;
esac
case "$os" in
  linux|darwin) ;;
  # No Windows branch of THIS script: it is sh, and the shell a Windows user
  # actually has is PowerShell — where `curl` is an alias for Invoke-WebRequest
  # and rejects -fsSL before this file is ever fetched. Anyone reaching this
  # line is in Git Bash, WSL or MSYS, and needs the commands that do work
  # rather than a bare "unsupported".
  *) cat >&2 <<'WINDOWS'
daybook: this installer supports linux and darwin only.

On Windows, run this in PowerShell instead:

  irm https://github.com/humanikio/daybook/releases/latest/download/install.ps1 | iex

Then open a NEW PowerShell window (Windows only reads PATH when a shell
starts) and run:

  daybook init
WINDOWS
     exit 1 ;;
esac

# GitHub release-asset URLs differ for "latest" vs a pinned tag:
#   latest → /releases/latest/download/<asset>
#   v0.1.0 → /releases/download/v0.1.0/<asset>
if [ -n "${DAYBOOK_DOWNLOAD_BASE:-}" ]; then
  asset_base="$DAYBOOK_DOWNLOAD_BASE"
elif [ "$VERSION" = "latest" ]; then
  asset_base="https://github.com/${REPO}/releases/latest/download"
else
  asset_base="https://github.com/${REPO}/releases/download/${VERSION}"
fi

url="${asset_base}/daybook-${os}-${arch}"
tmp="$(mktemp)"
echo "Downloading daybook (${os}/${arch}) ..."
curl -fsSL "$url" -o "$tmp"
chmod +x "$tmp"

dir="$(dirname "$DEST")"
mkdir -p "$dir"
if [ -w "$dir" ]; then
  mv "$tmp" "$DEST"
else
  echo "Elevating to install into $dir ..."
  sudo mv "$tmp" "$DEST"
fi

echo ""
echo "Installed: $DEST"

# A binary that is installed but not on PATH is the single most common way an
# install "fails" while reporting success. Check, and say exactly what to add.
case ":${PATH}:" in
  *":${dir}:"*) ;;
  *)
    echo ""
    echo "  ! $dir is not on your PATH."
    echo "    Add it, then open a new shell:"
    echo ""
    case "${SHELL##*/}" in
      zsh)  echo "      echo 'export PATH=\"$dir:\$PATH\"' >> ~/.zshrc" ;;
      bash) echo "      echo 'export PATH=\"$dir:\$PATH\"' >> ~/.bashrc" ;;
      fish) echo "      fish_add_path $dir" ;;
      *)    echo "      export PATH=\"$dir:\$PATH\"" ;;
    esac
    ;;
esac

echo ""
echo "Next — guided setup. It asks where your repos live and when to run,"
echo "then offers to schedule it. Nothing is installed without asking."
echo ""
echo "  daybook init"
echo ""
