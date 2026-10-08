# Second Brain installer for Windows.
#
#   irm https://raw.githubusercontent.com/sergiofreitas/leader-second-brain/main/install.ps1 | iex
#
# Downloads the second-brain binary from GitHub Releases, verifies its
# SHA-256 against the release's checksums.txt, installs it in
# %USERPROFILE%\.second-brain\bin and adds that folder to the user PATH.
#
# Options (environment variables, since `irm | iex` takes no parameters):
#   SECOND_BRAIN_VERSION         release tag to install (default: the latest)
#   SECOND_BRAIN_INSTALL_DIR     install folder (default: %USERPROFILE%\.second-brain\bin)
#   SECOND_BRAIN_NO_MODIFY_PATH  set to 1 to leave the user PATH alone
#   SECOND_BRAIN_BASE_URL        download from this mirror of the release files
#   SECOND_BRAIN_BINARY          install this local file instead of downloading
#                                (for testing a build before releasing it)

& {
    $ErrorActionPreference = 'Stop'
    $ProgressPreference = 'SilentlyContinue'  # Invoke-WebRequest is much faster without it
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

    $repo = 'sergiofreitas/leader-second-brain'
    $asset = 'second-brain-windows-amd64.exe'
    $installDir = $env:SECOND_BRAIN_INSTALL_DIR
    if (-not $installDir) { $installDir = Join-Path $env:USERPROFILE '.second-brain\bin' }
    $target = Join-Path $installDir 'second-brain.exe'

    if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') {
        Write-Host 'Windows on ARM: installing the x64 build (it runs under emulation).'
    }

    New-Item -ItemType Directory -Force -Path $installDir | Out-Null
    $tmp = Join-Path ([IO.Path]::GetTempPath()) ("second-brain-" + [Guid]::NewGuid())
    New-Item -ItemType Directory -Path $tmp | Out-Null

    try {
        if ($env:SECOND_BRAIN_BINARY) {
            Write-Host "Installing the local binary $($env:SECOND_BRAIN_BINARY) (no download, no checksum)"
            Copy-Item $env:SECOND_BRAIN_BINARY (Join-Path $tmp $asset)
        } else {
            if ($env:SECOND_BRAIN_BASE_URL) {
                $base = $env:SECOND_BRAIN_BASE_URL.TrimEnd('/')
            } elseif ($env:SECOND_BRAIN_VERSION) {
                $base = "https://github.com/$repo/releases/download/$($env:SECOND_BRAIN_VERSION)"
            } else {
                $base = "https://github.com/$repo/releases/latest/download"
            }
            Write-Host "Downloading $asset from $base"
            Invoke-WebRequest -UseBasicParsing -Uri "$base/$asset" -OutFile (Join-Path $tmp $asset)
            Invoke-WebRequest -UseBasicParsing -Uri "$base/checksums.txt" -OutFile (Join-Path $tmp 'checksums.txt')

            $line = Get-Content (Join-Path $tmp 'checksums.txt') | Where-Object { $_ -match "\s\*?$([regex]::Escape($asset))$" }
            if (-not $line) { throw "checksums.txt has no entry for $asset" }
            $expected = ($line -split '\s+')[0].ToLower()
            $actual = (Get-FileHash -Algorithm SHA256 (Join-Path $tmp $asset)).Hash.ToLower()
            if ($expected -ne $actual) {
                throw "Checksum mismatch for $asset (expected $expected, got $actual). Not installing."
            }
            Write-Host 'Checksum verified.'
        }

        try {
            Move-Item -Force (Join-Path $tmp $asset) $target
        } catch {
            throw "Could not replace $target. Is it running? Close Claude Code (or any MCP host using it) and try again."
        }
    } finally {
        Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
    }

    # Add the install folder to the user PATH (new terminals and apps see it)
    if ($env:SECOND_BRAIN_NO_MODIFY_PATH -ne '1') {
        $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
        $entries = @($userPath -split ';' | Where-Object { $_ })
        if ($entries -notcontains $installDir) {
            [Environment]::SetEnvironmentVariable('Path', (($entries + $installDir) -join ';'), 'User')
            Write-Host "Added $installDir to your user PATH."
        }
        if (($env:Path -split ';') -notcontains $installDir) {
            $env:Path = "$env:Path;$installDir"
        }
    }

    $installed = & $target version
    Write-Host ""
    Write-Host "Installed $installed at $target"
    Write-Host ""
    Write-Host "Next steps:"
    Write-Host "  1. Create a config (optional; without one, search is by keyword only):"
    Write-Host "       second-brain init --help"
    Write-Host "  2. Install the Claude Code plugin:"
    Write-Host "       claude plugin marketplace add $repo"
    Write-Host "       claude plugin install second-brain@second-brain"
    Write-Host "  3. Restart Claude Code (open a new terminal first, so it sees the new PATH)."
}
