[CmdletBinding()]
param(
    [Parameter(Mandatory = $true, Position = 0)]
    [string]$Version,

    [ValidateSet("windows")]
    [string]$Platform = "windows",

    [ValidateSet("amd64")]
    [string]$Arch = "amd64",

    [string]$OutputDir
)

$ErrorActionPreference = "Stop"
$Version = $Version.TrimStart("v")
if ($Version -notmatch '^\d+\.\d+\.\d+$') {
    throw "Version must use MAJOR.MINOR.PATCH format"
}

$DesktopDir = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$SourceRoot = (Resolve-Path (Join-Path $DesktopDir "..")).Path
if (-not $OutputDir) {
    $OutputDir = Join-Path $DesktopDir "release"
}
if (-not [System.IO.Path]::IsPathRooted($OutputDir)) {
    throw "OutputDir must be an absolute path: $OutputDir"
}
$OutputDir = [System.IO.Path]::GetFullPath($OutputDir)

if (-not [Environment]::Is64BitOperatingSystem -or -not [Environment]::Is64BitProcess -or $env:PROCESSOR_ARCHITECTURE -ne "AMD64") {
    throw "Windows releases must be built natively on an amd64 Windows host"
}

if (-not (Get-Command "go" -ErrorAction SilentlyContinue)) {
    throw "Required tool not found: go. Install Go 1.23 or later from https://go.dev/dl/ and add it to PATH."
}
if (-not (Get-Command "npm" -ErrorAction SilentlyContinue)) {
    throw "Required tool not found: npm. Install Node.js 22 (including npm) from https://nodejs.org/."
}
if (-not (Get-Command "wails" -ErrorAction SilentlyContinue)) {
    $GoBin = Join-Path (& go env GOPATH).Trim() "bin"
    throw "Required tool not found: wails.`nInstall Wails CLI v2.12.0 with:`n  go install github.com/wailsapp/wails/v2/cmd/wails@v2.12.0`nThen add $GoBin to PATH and retry."
}
if (-not (Get-Command "bash" -ErrorAction SilentlyContinue)) {
    throw "Required tool not found: bash. Install Git for Windows from https://git-scm.com/download/win and ensure its bash.exe is in PATH."
}

$FrontendLock = Join-Path $SourceRoot "frontend\package-lock.json"
$TinyMceDir = Join-Path $SourceRoot "public\tinymce"
if (-not (Test-Path -PathType Leaf $FrontendLock)) {
    throw "Shared frontend not found at $(Join-Path $SourceRoot 'frontend')"
}
if (-not (Test-Path -PathType Container $TinyMceDir)) {
    throw "TinyMCE assets not found at $TinyMceDir"
}
if (-not (Test-Path -PathType Leaf (Join-Path $DesktopDir "build\appicon.png"))) {
    throw "Application icon not found at $(Join-Path $DesktopDir 'build\appicon.png')"
}
if (-not (Test-Path -PathType Leaf (Join-Path $DesktopDir "build\windows\icon.ico"))) {
    throw "Windows application icon not found at $(Join-Path $DesktopDir 'build\windows\icon.ico')"
}

$VersionFile = Join-Path $DesktopDir "api\version.go"
$VersionMatch = Select-String -Path $VersionFile -Pattern '^const ClientVersion = "([^"]*)"$'
$ClientVersion = if ($VersionMatch) { $VersionMatch.Matches[0].Groups[1].Value } else { "" }
if ($ClientVersion -ne $Version) {
    throw "Requested version $Version does not match api.ClientVersion $(if ($ClientVersion) { $ClientVersion } else { '<missing>' })"
}

$WailsVersion = (& wails version 2>&1 | Out-String).Trim()
if ($LASTEXITCODE -ne 0 -or $WailsVersion -notmatch '(^|[^0-9])v?2\.12\.0([^0-9]|$)') {
    throw "Wails CLI v2.12.0 is required; got: $WailsVersion"
}

New-Item -ItemType Directory -Force -Path $OutputDir | Out-Null

& npm ci --prefix (Join-Path $SourceRoot "frontend")
if ($LASTEXITCODE -ne 0) { throw "npm ci failed" }
& npm test --prefix (Join-Path $SourceRoot "frontend") -- --run
if ($LASTEXITCODE -ne 0) { throw "frontend tests failed" }
& npm run build --prefix (Join-Path $SourceRoot "frontend")
if ($LASTEXITCODE -ne 0) { throw "frontend build failed" }

Push-Location $DesktopDir
try {
    & go test ./...
    if ($LASTEXITCODE -ne 0) { throw "desktop tests failed" }
    & wails build -clean -platform "$Platform/$Arch" --nsis
    if ($LASTEXITCODE -ne 0) { throw "Wails build failed" }
}
finally {
    Pop-Location
}

$Installer = Get-ChildItem -Path (Join-Path $DesktopDir "build\bin") -Filter "*-installer.exe" -File |
    Sort-Object LastWriteTime -Descending | Select-Object -First 1
if (-not $Installer) {
    throw "NSIS installer not found in $(Join-Path $DesktopDir 'build\bin')"
}
$Archive = Join-Path $OutputDir "gemsnote-$Version-$Platform-$Arch-installer.exe"
Copy-Item -Path $Installer.FullName -Destination $Archive -Force

$ChecksumLines = Get-ChildItem -Path $OutputDir -File |
    Where-Object { $_.Name -match "^gemsnote-$([regex]::Escape($Version))-.*\.(exe|dmg|AppImage|zip)$" } |
    Sort-Object Name |
    ForEach-Object {
        $Hash = (Get-FileHash -Algorithm SHA256 -Path $_.FullName).Hash.ToLowerInvariant()
        "$Hash  $($_.Name)"
    }
if (-not $ChecksumLines) {
    throw "No release archives found in $OutputDir"
}
$ChecksumFile = Join-Path $OutputDir "SHA256SUMS"
$ChecksumLines | Set-Content -Path $ChecksumFile -Encoding ascii

Write-Host "Created $Archive"
Write-Host "Updated $ChecksumFile"
