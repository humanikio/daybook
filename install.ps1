# daybook installer for Windows
#   irm https://github.com/humanikio/daybook/releases/latest/download/install.ps1 | iex
#
# Downloads the prebuilt binary and installs it under LOCALAPPDATA, then adds
# that directory to the USER PATH. Does not run setup — `daybook init` does.
$ErrorActionPreference = "Stop"

$version = if ($env:DAYBOOK_VERSION) { $env:DAYBOOK_VERSION } else { "latest" }
$repo    = if ($env:DAYBOOK_REPO)    { $env:DAYBOOK_REPO }    else { "humanikio/daybook" }

# Per-user, not Program Files: daybook runs as you and needs no elevation, and
# asking for admin to install a personal reporting tool is a bad first
# impression.
$dir = if ($env:DAYBOOK_DIR_BIN) { $env:DAYBOOK_DIR_BIN } else { "$env:LOCALAPPDATA\Programs\daybook" }

$arch = if ([Environment]::Is64BitOperatingSystem) {
    if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64" -or $env:PROCESSOR_ARCHITEW6432 -eq "ARM64") { "arm64" } else { "amd64" }
} else {
    throw "daybook: 32-bit Windows is not supported"
}

$base = if ($version -eq "latest") {
    "https://github.com/$repo/releases/latest/download"
} else {
    "https://github.com/$repo/releases/download/$version"
}

New-Item -ItemType Directory -Force -Path $dir | Out-Null
$dest = Join-Path $dir "daybook.exe"

$asset = "daybook-windows-$arch.exe"

# Downloaded to a temporary file and verified BEFORE it is put on your PATH.
# This used to write straight to $dest, so a corrupted or tampered download was
# already installed and runnable by the time anything could have noticed.
$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ([System.IO.Path]::GetRandomFileName())

Write-Host "Downloading daybook (windows/$arch) ..."

# Two things Windows PowerShell 5.1 — the default shell on Windows 10 and 11 —
# needs, learned the hard way in humanikd's installer:
#
#   -UseBasicParsing: without it Invoke-WebRequest uses the Internet Explorer
#   parsing engine, and FAILS OUTRIGHT on a machine where IE first-launch
#   configuration has never run. That is the normal state of a fresh install and
#   of most Windows Server images, so the installer breaks for exactly the people
#   installing it for the first time.
#
#   $ProgressPreference: the progress bar makes a large download roughly an order
#   of magnitude slower on 5.1. Restored afterwards rather than left off, because
#   this script is often dot-sourced into a session the user keeps using.
$prevProgress = $ProgressPreference
$ProgressPreference = 'SilentlyContinue'
try {
    Invoke-WebRequest -Uri "$base/$asset" -OutFile $tmp -UseBasicParsing
} finally {
    $ProgressPreference = $prevProgress
}

if ($env:DAYBOOK_SKIP_VERIFY -eq "1") {
    Write-Host "  ! skipping checksum verification (DAYBOOK_SKIP_VERIFY=1)"
} else {
    # Fails closed. A checksum that cannot be fetched stops the install rather
    # than proceeding quietly.
    $sums = Join-Path ([System.IO.Path]::GetTempPath()) ([System.IO.Path]::GetRandomFileName())
    try {
        Invoke-WebRequest -Uri "$base/checksums.txt" -OutFile $sums -UseBasicParsing
    } catch {
        Remove-Item $tmp -Force -ErrorAction SilentlyContinue
        throw "Could not fetch checksums.txt, so the download cannot be verified."
    }

    # Read from a FILE, not from a response body. On 5.1 an octet-stream response
    # comes back as [byte[]], and splitting that stringifies it to decimal byte
    # values — so the "hash" parsed out is "52" and never matches, failing every
    # good download. -OutFile sidesteps it entirely.
    #
    # The comparison and its abort live OUTSIDE any try block on purpose: under
    # $ErrorActionPreference='Stop' a throw inside one is caught by that block's
    # catch, and humanikd's installer spent a release swallowing its own mismatch
    # abort into a catch written for a missing checksum.
    #
    # sha256sum writes "<hash>  <name>". Match the name at the end of the line so
    # a name that merely contains this one cannot match.
    # Built with single quotes and concatenation on purpose. Interpolating the
    # asset name into a double-quoted pattern puts $asset immediately before the
    # $ anchor, and how that parses is not something to guess at in a script that
    # decides whether a binary is trustworthy.
    $pattern = '^([0-9a-fA-F]{64})\s+\*?' + [regex]::Escape($asset) + '$'
    $want = $null
    foreach ($line in Get-Content $sums) {
        if ($line -match $pattern) {
            $want = $Matches[1]
            break
        }
    }
    Remove-Item $sums -Force -ErrorAction SilentlyContinue
    if (-not $want) {
        Remove-Item $tmp -Force -ErrorAction SilentlyContinue
        throw "checksums.txt does not list $asset."
    }

    $got = (Get-FileHash -Algorithm SHA256 -Path $tmp).Hash
    if ($got -ne $want.ToUpper()) {
        Remove-Item $tmp -Force -ErrorAction SilentlyContinue
        Write-Host "  expected $want"
        Write-Host "  got      $got"
        throw "CHECKSUM MISMATCH for $asset. Not installing. This is either a corrupted download or a tampered one."
    }
    Write-Host "  checksum ok"
}

Move-Item -Path $tmp -Destination $dest -Force

# Only append to PATH if it is not already there — running the installer twice
# should not leave two copies of the same directory in the user's PATH.
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($userPath -notlike "*$dir*") {
    [Environment]::SetEnvironmentVariable("Path", "$userPath;$dir", "User")
    Write-Host ""
    Write-Host "  Added $dir to your PATH."
    Write-Host "  Open a NEW PowerShell window — Windows only reads PATH when a shell starts."
}

Write-Host ""
Write-Host "Installed: $dest"
Write-Host ""
Write-Host "Next — guided setup. It asks where your repos live and when to run,"
Write-Host "then offers to schedule it. Nothing is installed without asking."
Write-Host ""
Write-Host "  daybook init"
Write-Host ""
