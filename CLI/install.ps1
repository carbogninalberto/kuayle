# Install the official Kuayle CLI on Windows from a GitHub release.
#
#   irm https://raw.githubusercontent.com/carbogninalberto/kuayle/main/CLI/install.ps1 | iex
#
# KUAYLE_VERSION       a CLI release tag such as cli-v0.2.0 (default: newest)
# KUAYLE_INSTALL_DIR   where to put kuayle.exe (default: %LOCALAPPDATA%\Programs\kuayle)
#
# Downloads come only from github.com/carbogninalberto/kuayle. The archive is
# checked against the release's checksums.txt (SHA-256) before it is
# extracted; a missing entry or a mismatch aborts without installing.

$ErrorActionPreference = "Stop"

$Repo = "carbogninalberto/kuayle"
$InstallDir = if ($env:KUAYLE_INSTALL_DIR) { $env:KUAYLE_INSTALL_DIR } else { "$env:LOCALAPPDATA\Programs\kuayle" }
$Version = if ($env:KUAYLE_VERSION) { $env:KUAYLE_VERSION } else { "latest" }

# Only x86_64 is published for Windows (arm64 runs it via emulation).
$Arch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture
if ($Arch -notin @("X64", "Arm64")) { throw "Unsupported architecture: $Arch" }

if ($Version -eq "latest") {
    # Newest stable release whose tag starts with cli-v (server releases use v*).
    $Releases = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases?per_page=100" -Headers @{ "User-Agent" = "kuayle-install" }
    $Version = ($Releases | Where-Object { -not $_.draft -and -not $_.prerelease -and $_.tag_name -like "cli-v*" } | Select-Object -First 1).tag_name
}
if ($Version -notmatch '^cli-v\d+\.\d+\.\d+$') { throw "Invalid or missing CLI release tag: '$Version'" }

$Archive = "kuayle-x86_64-pc-windows-msvc.zip"
$Base = "https://github.com/$Repo/releases/download/$Version"

$Tmp = New-Item -ItemType Directory -Force -Path ([IO.Path]::Combine([IO.Path]::GetTempPath(), [Guid]::NewGuid().ToString()))
try {
    Write-Host "Downloading kuayle $Version..."
    $ZipPath = Join-Path $Tmp $Archive
    $SumsPath = Join-Path $Tmp "checksums.txt"
    Invoke-WebRequest -UseBasicParsing -Uri "$Base/$Archive" -OutFile $ZipPath
    Invoke-WebRequest -UseBasicParsing -Uri "$Base/checksums.txt" -OutFile $SumsPath

    $Expected = $null
    foreach ($Line in Get-Content $SumsPath) {
        $Fields = $Line -split '\s+', 2
        if ($Fields.Count -eq 2 -and (($Fields[1].TrimStart('*') -split '/')[-1]) -eq $Archive) {
            $Expected = $Fields[0].ToLowerInvariant(); break
        }
    }
    if ($Expected -notmatch '^[0-9a-f]{64}$') { throw "checksums.txt has no entry for $Archive; refusing to install" }
    $Actual = (Get-FileHash -Algorithm SHA256 -Path $ZipPath).Hash.ToLowerInvariant()
    if ($Actual -ne $Expected) { throw "Checksum mismatch for $Archive (expected $Expected, got $Actual); refusing to install" }
    Write-Host "✓ SHA-256 verified"

    Expand-Archive -Path $ZipPath -DestinationPath $Tmp -Force
    New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
    Move-Item -Force (Join-Path $Tmp "kuayle.exe") (Join-Path $InstallDir "kuayle.exe")
} finally {
    Remove-Item -Recurse -Force $Tmp
}

Write-Host "✓ kuayle $Version installed to $InstallDir\kuayle.exe"

$UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($UserPath -notlike "*$InstallDir*") {
    [Environment]::SetEnvironmentVariable("Path", "$UserPath;$InstallDir", "User")
    Write-Host "✓ Added $InstallDir to your user PATH (restart your terminal to take effect)."
}
