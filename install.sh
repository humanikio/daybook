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

asset="daybook-${os}-${arch}"
url="${asset_base}/${asset}"
tmp="$(mktemp)"
echo "Downloading daybook (${os}/${arch}) ..."
curl -fsSL "$url" -o "$tmp"

# VERIFY BEFORE INSTALLING. This script used to download a binary and move it
# onto your PATH without checking anything, while the README said every release
# was signed and shipped with checksums. Both were published; neither was read.
#
# Fails closed. A checksum that cannot be fetched or computed stops the install
# rather than proceeding quietly — DAYBOOK_SKIP_VERIFY=1 is the way out, and it
# is a deliberate thing to type.
if [ "${DAYBOOK_SKIP_VERIFY:-0}" = "1" ]; then
  echo "  ! skipping checksum verification (DAYBOOK_SKIP_VERIFY=1)"
else
  if command -v sha256sum >/dev/null 2>&1; then
    sum="$(sha256sum "$tmp" | awk '{print $1}')"
  elif command -v shasum >/dev/null 2>&1; then
    sum="$(shasum -a 256 "$tmp" | awk '{print $1}')"
  else
    rm -f "$tmp"
    echo "No sha256 tool found, so the download cannot be verified." >&2
    echo "Install coreutils, or re-run with DAYBOOK_SKIP_VERIFY=1." >&2
    exit 1
  fi

  sums="$(mktemp)"
  if ! curl -fsSL "${asset_base}/checksums.txt" -o "$sums"; then
    rm -f "$tmp" "$sums"
    echo "Could not fetch checksums.txt, so the download cannot be verified." >&2
    exit 1
  fi
  # The two-space separator is sha256sum's own format. Anchor on it so a name
  # that merely contains this one cannot match.
  want="$(grep "  ${asset}\$" "$sums" | awk '{print $1}')"
  rm -f "$sums"
  if [ -z "$want" ]; then
    rm -f "$tmp"
    echo "checksums.txt does not list ${asset}." >&2
    exit 1
  fi
  if [ "$sum" != "$want" ]; then
    rm -f "$tmp"
    echo "CHECKSUM MISMATCH for ${asset}." >&2
    echo "  expected $want" >&2
    echo "  got      $sum" >&2
    echo "Not installing. This is either a corrupted download or a tampered one." >&2
    exit 1
  fi
  echo "  checksum ok"
fi

# The signature proves WHO built it, which the checksum cannot: a checksums file
# and a binary can be replaced together. Only run when cosign is already here —
# installing a verification tool as part of an unverified install is circular.
if [ "${DAYBOOK_SKIP_VERIFY:-0}" != "1" ] && command -v cosign >/dev/null 2>&1; then
  sig="$(mktemp)"; crt="$(mktemp)"
  if curl -fsSL "${asset_base}/${asset}.sig" -o "$sig" &&
     curl -fsSL "${asset_base}/${asset}.pem" -o "$crt"; then
    # cosign writes the certificate BASE64-ENCODED, not as raw PEM. Whether
    # verify-blob accepts that form back is not something to depend on: getting
    # it wrong rejects a genuine release, which is worse than not checking at
    # all. Normalise to real PEM here so the format cannot matter.
    if ! head -c 11 "$crt" 2>/dev/null | grep -q -- "-----BEGIN"; then
      if base64 -d < "$crt" > "${crt}.dec" 2>/dev/null ||
         base64 -D < "$crt" > "${crt}.dec" 2>/dev/null; then
        if head -c 11 "${crt}.dec" 2>/dev/null | grep -q -- "-----BEGIN"; then
          mv "${crt}.dec" "$crt"
        fi
      fi
      rm -f "${crt}.dec"
    fi
    if cosign verify-blob \
         --certificate "$crt" --signature "$sig" \
         --certificate-identity-regexp "^https://github.com/${REPO}/\.github/workflows/release\.yml@refs/tags/" \
         --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
         "$tmp" >/dev/null 2>&1; then
      echo "  signature ok"
    else
      rm -f "$tmp" "$sig" "$crt"
      echo "SIGNATURE VERIFICATION FAILED for ${asset}. Not installing." >&2
      exit 1
    fi
  else
    # Releases before v0.3.5 published a .sig with no certificate, which nothing
    # could verify. Missing is not the same as wrong.
    echo "  ! no signature published for this release — checksum only"
  fi
  rm -f "$sig" "$crt"
fi

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
