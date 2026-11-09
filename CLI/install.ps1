# kuayle-cli installer for Windows — downloads the latest release binary.
# kuayle-cli Windows 安装器 — 下载最新 release 二进制。
#
# Usage / 用法:
#   irm https://raw.githubusercontent.com/uiYzzi/kuayle_cli/main/install.ps1 | iex

$ErrorActionPreference = "Stop"

$InstallDir = if ($env:KUAYLE_INSTALL_DIR) { $env:KUAYLE_INSTALL_DIR } else { "$env:LOCALAPPDATA\Programs\kuayle" }
$Repo = "uiYzzi/kuayle_cli"
$Version = if ($env:KUAYLE_VERSION) { $env:KUAYLE_VERSION } else { "latest" }

# Only x86_64 is published for Windows (arm64 runs it via emulation).
# Windows 只发布 x86_64 构建(arm64 可通过仿真运行)。
$Arch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture
if ($Arch -notin @("X64", "Arm64")) {
    Write-Error "Unsupported arch: $Arch"; exit 1
}

$Archive = "kuayle-x86_64-pc-windows-msvc.zip"
if ($Version -eq "latest") {
    $Url = "https://github.com/$Repo/releases/latest/download/$Archive"
} else {
    $Url = "https://github.com/$Repo/releases/download/$Version/$Archive"
}

Write-Host "Installing kuayle to $InstallDir..."
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null

$Tmp = New-Item -ItemType Directory -Force -Path ([IO.Path]::Combine([IO.Path]::GetTempPath(), [Guid]::NewGuid().ToString()))
try {
    $ZipPath = Join-Path $Tmp "kuayle.zip"
    Invoke-WebRequest -Uri $Url -OutFile $ZipPath
    Expand-Archive -Path $ZipPath -DestinationPath $Tmp -Force
    Move-Item -Force (Join-Path $Tmp "kuayle.exe") (Join-Path $InstallDir "kuayle.exe")
} finally {
    Remove-Item -Recurse -Force $Tmp
}

Write-Host "✓ kuayle installed to $InstallDir\kuayle.exe"

# Offer to add InstallDir to the user PATH if missing.
# 如果安装目录不在用户 PATH 中,提示添加。
$UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($UserPath -notlike "*$InstallDir*") {
    [Environment]::SetEnvironmentVariable("Path", "$UserPath;$InstallDir", "User")
    Write-Host "✓ Added $InstallDir to your user PATH (restart your terminal to take effect)."
    Write-Host "✓ 已将 $InstallDir 加入用户 PATH(重开终端生效)。"
}
