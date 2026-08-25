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

Write-Host "Downloading daybook (windows/$arch) ..."
Invoke-WebRequest -Uri "$base/daybook-windows-$arch.exe" -OutFile $dest

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
