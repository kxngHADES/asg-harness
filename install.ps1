# ASG installer for Windows (PowerShell, Windows Terminal, cmd).
#
#   irm https://raw.githubusercontent.com/kxngHADES/asg-harness/main/install.ps1 | iex
#
# Environment:
#   $env:ASG_VERSION = "v0.2.0"     install a specific release (default: latest)
#   $env:ASG_INSTALL_DIR = "C:\bin" install location (default: %LOCALAPPDATA%\Programs\asg)
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

$repo = 'kxngHADES/asg-harness'
$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
  'AMD64' { 'amd64' }
  'ARM64' { 'arm64' }
  default { throw "Unsupported architecture: $env:PROCESSOR_ARCHITECTURE" }
}
$asset = "asg_windows_$arch.zip"
$base = if ($env:ASG_VERSION) { "https://github.com/$repo/releases/download/$($env:ASG_VERSION)" }
        else { "https://github.com/$repo/releases/latest/download" }
$dir = if ($env:ASG_INSTALL_DIR) { $env:ASG_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'Programs\asg' }

$tmp = Join-Path ([IO.Path]::GetTempPath()) ("asg-" + [guid]::NewGuid())
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
  Write-Host "▸ downloading $asset" -ForegroundColor Green
  Invoke-WebRequest "$base/$asset" -OutFile "$tmp\$asset" -UseBasicParsing
  Invoke-WebRequest "$base/checksums.txt" -OutFile "$tmp\checksums.txt" -UseBasicParsing

  $line = Select-String -Path "$tmp\checksums.txt" -Pattern " $([regex]::Escape($asset))$" | Select-Object -First 1
  if (-not $line) { throw "$asset is not listed in checksums.txt" }
  $expected = ($line.Line -split '\s+')[0]
  $actual = (Get-FileHash "$tmp\$asset" -Algorithm SHA256).Hash.ToLower()
  if ($expected -ne $actual) { throw "Checksum mismatch for $asset" }

  Expand-Archive "$tmp\$asset" -DestinationPath $tmp -Force
  New-Item -ItemType Directory -Path $dir -Force | Out-Null
  Copy-Item "$tmp\asg.exe" (Join-Path $dir 'asg.exe') -Force
} finally {
  Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
}

$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (-not (($userPath -split ';') -contains $dir)) {
  [Environment]::SetEnvironmentVariable('Path', ($userPath.TrimEnd(';') + ";$dir").TrimStart(';'), 'User')
  $env:Path += ";$dir"
  Write-Host "▸ added $dir to your PATH (open a new terminal to pick it up everywhere)" -ForegroundColor Green
}
& (Join-Path $dir 'asg.exe') version
Write-Host "▸ run 'asg' for the terminal agent, or 'asg dashboard' for the web version" -ForegroundColor Green
