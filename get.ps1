<#
.SYNOPSIS
    Install the psl compiler from its latest GitHub release, on Windows.

.DESCRIPTION
    No Go toolchain is needed: this downloads the binary built for your
    platform, checks it against the release's SHA256SUMS — a release it cannot
    verify is never installed — and puts it in a directory on your PATH.

    From PowerShell:

        irm https://raw.githubusercontent.com/lhypds/psl/main/get.ps1 | iex

    From cmd.exe:

        powershell -NoProfile -Command "irm https://raw.githubusercontent.com/lhypds/psl/main/get.ps1 | iex"

    To pass options through the pipe, create the script block yourself:

        &([scriptblock]::Create((irm https://raw.githubusercontent.com/lhypds/psl/main/get.ps1))) -InstallDir C:\tools\psl

.PARAMETER Version
    Version to install, with or without the leading "v". Defaults to the
    latest release, or to $env:PSL_VERSION when that is set.

.PARAMETER InstallDir
    Directory to install psl.exe into. Defaults to $env:PSL_INSTALL_DIR, or
    %LOCALAPPDATA%\Programs\psl — a per-user location, so no administrator
    rights are needed.

.PARAMETER Repo
    GitHub repository to download from, as "owner/name".

.PARAMETER NoPath
    Do not touch the user PATH.
#>

param(
    [string] $Version    = $env:PSL_VERSION,
    [string] $InstallDir = $env:PSL_INSTALL_DIR,
    [string] $Repo       = $(if ($env:PSL_REPO) { $env:PSL_REPO } else { 'lhypds/psl' }),
    [switch] $NoPath
)

$ErrorActionPreference = 'Stop'
# Invoke-WebRequest's progress bar makes downloads several times slower on
# Windows PowerShell, and TLS 1.2 is not its default there either.
$ProgressPreference = 'SilentlyContinue'
try {
    [Net.ServicePointManager]::SecurityProtocol =
        [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
} catch {
    # PowerShell 7 negotiates TLS itself and has no such setting.
}

$UserAgent = 'psl-installer'

function Fail($message) {
    Write-Host "get.ps1: $message" -ForegroundColor Red
    exit 1
}

function Save-File($url, $path) {
    Invoke-WebRequest -Uri $url -OutFile $path -UserAgent $UserAgent -UseBasicParsing
}

# ── platform ────────────────────────────────────────────────────────────────
# PROCESSOR_ARCHITECTURE describes the running process, so a 32-bit PowerShell
# on 64-bit Windows reports x86; PROCESSOR_ARCHITEW6432 is the machine's own.
$osArch = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
switch ($osArch) {
    'AMD64' { $goarch = 'amd64' }
    'ARM64' { $goarch = 'arm64' }
    default { Fail "unsupported architecture `"$osArch`" — build from source instead: https://github.com/$Repo" }
}

# ── version ─────────────────────────────────────────────────────────────────
if (-not $Version) {
    $headers = @{ 'Accept' = 'application/vnd.github+json' }
    # A token is not required, but it lifts GitHub's anonymous rate limit.
    if ($env:GITHUB_TOKEN) { $headers['Authorization'] = "Bearer $env:GITHUB_TOKEN" }
    try {
        $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest" `
            -Headers $headers -UserAgent $UserAgent
        $Version = $release.tag_name
    } catch {
        Fail "could not work out the latest release of ${Repo}: $($_.Exception.Message)"
    }
}
if (-not $Version) { Fail "$Repo has no published release yet; pass -Version <ver>" }
$Version = $Version -replace '^v', ''

if (-not $InstallDir) { $InstallDir = Join-Path $env:LOCALAPPDATA 'Programs\psl' }

$base = "https://github.com/$Repo/releases/download/v$Version"
$temp = Join-Path ([IO.Path]::GetTempPath()) ("psl-install-" + [guid]::NewGuid().ToString('n'))
New-Item -ItemType Directory -Path $temp -Force | Out-Null

try {
    # ── checksums ───────────────────────────────────────────────────────────
    # Read the checksums first: they also say which archives this release has,
    # which is how an arm64 machine finds out whether it has a native build.
    # They go through a file rather than a string, because a release asset is
    # served as application/octet-stream and comes back as bytes.
    $sumsPath = Join-Path $temp 'SHA256SUMS'
    try {
        Save-File "$base/SHA256SUMS" $sumsPath
    } catch {
        Fail "release v$Version of $Repo has no SHA256SUMS; psl will not install a release it cannot verify"
    }
    $sums = @{}
    foreach ($line in ((Get-Content -LiteralPath $sumsPath -Raw) -split "`n")) {
        $fields = $line.Trim() -split '\s+'
        # Each line is "<hash>  <asset>"; binary mode marks the name with "*".
        if ($fields.Count -eq 2) { $sums[$fields[1].TrimStart('*')] = $fields[0].ToLower() }
    }

    $archive = "psl-$Version-windows-$goarch.zip"
    if (-not $sums.ContainsKey($archive) -and $goarch -eq 'arm64') {
        # Windows runs x64 binaries on ARM64 under emulation, so a release
        # without a native build is still installable.
        $archive = "psl-$Version-windows-amd64.zip"
        Write-Host "get.ps1: release v$Version has no arm64 build; installing the amd64 one (Windows will emulate it)"
    }
    if (-not $sums.ContainsKey($archive)) {
        Fail "release v$Version of $Repo has no Windows build for $goarch — see https://github.com/$Repo/releases"
    }

    # ── download and verify ─────────────────────────────────────────────────
    Write-Host "get.ps1: downloading psl $Version for windows/$goarch..."
    $zip = Join-Path $temp $archive
    try {
        Save-File "$base/$archive" $zip
    } catch {
        Fail "could not download $base/${archive}: $($_.Exception.Message)"
    }

    $got = (Get-FileHash -LiteralPath $zip -Algorithm SHA256).Hash.ToLower()
    if ($got -ne $sums[$archive]) {
        Fail "checksum mismatch for ${archive}: got $got, want $($sums[$archive])"
    }

    # ── unpack ──────────────────────────────────────────────────────────────
    $unpacked = Join-Path $temp 'unpacked'
    if (Get-Command Expand-Archive -ErrorAction SilentlyContinue) {
        Expand-Archive -LiteralPath $zip -DestinationPath $unpacked -Force
    } else {
        Add-Type -AssemblyName System.IO.Compression.FileSystem
        [IO.Compression.ZipFile]::ExtractToDirectory($zip, $unpacked)
    }
    $binary = Get-ChildItem -LiteralPath $unpacked -Filter 'psl.exe' -Recurse -File | Select-Object -First 1
    if (-not $binary) { Fail "$archive contains no psl.exe" }

    # ── install ─────────────────────────────────────────────────────────────
    # New-Item also normalises the directory, so a relative -InstallDir and a
    # trailing backslash both end up as one absolute path for the PATH check.
    $InstallDir = (New-Item -ItemType Directory -Path $InstallDir -Force).FullName
    $target = Join-Path $InstallDir 'psl.exe'
    if (Test-Path -LiteralPath $target) {
        # Windows will not overwrite a running executable, but it will let it
        # be renamed out of the way.
        $old = "$target.old"
        Remove-Item -LiteralPath $old -Force -ErrorAction SilentlyContinue
        try {
            Move-Item -LiteralPath $target -Destination $old -Force
        } catch {
            Fail "psl.exe in $InstallDir is in use; close any running psl and try again"
        }
    }
    try {
        Copy-Item -LiteralPath $binary.FullName -Destination $target -Force
    } catch {
        Fail "could not write ${target}: $($_.Exception.Message)"
    }
    Remove-Item -LiteralPath "$target.old" -Force -ErrorAction SilentlyContinue

    $reported = & $target --version
    Write-Host "get.ps1: installed $target ($reported)" -ForegroundColor Green

    # ── PATH ────────────────────────────────────────────────────────────────
    if (-not $NoPath) {
        $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
        $entries = @()
        if ($userPath) { $entries = @($userPath -split ';' | Where-Object { $_ }) }
        if ($entries -notcontains $InstallDir) {
            [Environment]::SetEnvironmentVariable('Path', (($entries + $InstallDir) -join ';'), 'User')
            Write-Host "get.ps1: added $InstallDir to your user PATH"
            Write-Host "get.ps1: open a new terminal for `"psl`" to be found"
        }
        # Make it work in this session too, without waiting for a new terminal.
        if (($env:Path -split ';') -notcontains $InstallDir) { $env:Path = "$env:Path;$InstallDir" }
    } else {
        Write-Host "get.ps1: leaving PATH alone; run psl as $target"
    }

    if (-not $env:OPENAI_API_KEY -and -not $env:ANTHROPIC_API_KEY -and
        -not (Test-Path -LiteralPath (Join-Path $PWD '.pslrc')) -and
        -not (Test-Path -LiteralPath (Join-Path $HOME '.pslrc'))) {
        Write-Host ""
        Write-Host "next: set OPENAI_API_KEY or ANTHROPIC_API_KEY, or write a .pslrc"
        Write-Host "      (run: psl config)"
    }
} finally {
    Remove-Item -LiteralPath $temp -Recurse -Force -ErrorAction SilentlyContinue
}
