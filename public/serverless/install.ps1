# Install the Runware CLI, or reuse one that is already installed, then create
# an App directory with 'runware serverless init'.
#
#   irm https://serverless.runware.ai/install.ps1 | iex
#
# The script never runs pip, uv or python: init installs the Serverless
# library into the App directory's own environment (docs/adr/0001).
#
# Environment overrides:
#   RUNWARE_APP_DIR           the App directory (default: my-app)
#   RUNWARE_INSTALL_DIR       where the binary goes (default:
#                             %LOCALAPPDATA%\Programs\runware)
#   RUNWARE_RELEASE_BASE_URL  where releases come from (default: the GitHub
#                             releases of Runware/runware-cli). With an
#                             override, the script reads the latest version
#                             from <base>/latest, as JSON with a "tag_name"
#                             field, and the release files from
#                             <base>/download/<tag>/<file>.
#
# The script also runs under pwsh on Linux and macOS, where it installs the
# tar.gz archive. The tests use this.
#
# Keep this file ASCII: Windows PowerShell can decode a download without a
# charset as Latin-1. Errors throw instead of calling exit, because exit
# would close the user's shell under iex. The script block keeps the
# preferences set here out of that shell.

& {
    $ErrorActionPreference = 'Stop'
    $ProgressPreference = 'SilentlyContinue'

    $repo = 'Runware/runware-cli'
    # $IsWindows does not exist before PowerShell 6, which runs only on
    # Windows.
    $onWindows = ($PSVersionTable.PSVersion.Major -lt 6) -or $IsWindows
    $exe = if ($onWindows) { 'runware.exe' } else { 'runware' }

    if ($PSVersionTable.PSVersion.Major -lt 6) {
        # GitHub accepts only TLS 1.2 or newer.
        [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
    }

    $appDir = if ($env:RUNWARE_APP_DIR) { $env:RUNWARE_APP_DIR } else { 'my-app' }

    if ($env:RUNWARE_RELEASE_BASE_URL) {
        $releaseBase = $env:RUNWARE_RELEASE_BASE_URL.TrimEnd('/')
        $latestUrl = "$releaseBase/latest"
    } else {
        $releaseBase = "https://github.com/$repo/releases"
        $latestUrl = "https://api.github.com/repos/$repo/releases/latest"
    }

    $installDir = if ($env:RUNWARE_INSTALL_DIR) {
        $env:RUNWARE_INSTALL_DIR
    } elseif ($onWindows) {
        Join-Path $env:LOCALAPPDATA 'Programs\runware'
    } else {
        Join-Path $HOME '.local/bin'
    }

    # Test-Init reports whether the CLI at $Path has 'serverless init'. The
    # exit status alone cannot tell: a CLI from before init prints the help
    # of the serverless group for 'serverless init --help' and exits 0. Only
    # init's own help names the init command in its usage line.
    function Test-Init([string]$Path) {
        try {
            $help = & $Path serverless init --help 2>$null | Out-String
        } catch {
            return $false
        }
        return $help -match 'serverless init'
    }

    function Find-Runware {
        Get-Command $exe -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1 -ExpandProperty Source
    }

    # Get-ScoopShims returns the directory of Scoop's shims, or nothing when
    # Scoop is not installed. The scoop command itself is a shim there.
    function Get-ScoopShims {
        $scoop = Get-Command scoop -ErrorAction SilentlyContinue | Select-Object -First 1
        if ($scoop) { Split-Path -Parent $scoop.Source }
    }

    function Get-Arch {
        $arch = try {
            [Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
        } catch {
            $env:PROCESSOR_ARCHITECTURE
        }
        switch ($arch) {
            'X64' { return 'amd64' }
            'AMD64' { return 'amd64' }
            'Arm64' { return 'arm64' }
            default { throw "Unsupported architecture: $arch" }
        }
    }

    # Install-FromRelease puts the latest release, after checksum
    # verification, into $Dir and returns the absolute path of the binary.
    function Install-FromRelease([string]$Dir) {
        $os = if ($onWindows) { 'windows' } elseif ($IsMacOS) { 'darwin' } else { 'linux' }
        $ext = if ($onWindows) { 'zip' } else { 'tar.gz' }
        $arch = Get-Arch
        Write-Host "==> Detected OS: $os, Architecture: $arch"

        Write-Host '==> Fetching latest version...'
        $tag = (Invoke-RestMethod -Uri $latestUrl -UseBasicParsing).tag_name
        if (-not $tag) { throw 'Could not fetch latest release.' }
        Write-Host "==> Latest version is $tag"

        $archive = "runware_${os}_${arch}.$ext"
        $tmp = Join-Path ([IO.Path]::GetTempPath()) ('runware-' + [guid]::NewGuid())
        New-Item -ItemType Directory -Path $tmp | Out-Null
        try {
            $archivePath = Join-Path $tmp $archive
            $checksumsPath = Join-Path $tmp 'checksums.txt'
            Write-Host "==> Downloading $archive..."
            Invoke-WebRequest -Uri "$releaseBase/download/$tag/$archive" -OutFile $archivePath -UseBasicParsing
            Invoke-WebRequest -Uri "$releaseBase/download/$tag/checksums.txt" -OutFile $checksumsPath -UseBasicParsing

            $expected = Get-Content -LiteralPath $checksumsPath | ForEach-Object {
                $fields = -split $_
                if ($fields.Count -eq 2 -and $fields[1] -eq $archive) { $fields[0] }
            } | Select-Object -First 1
            if (-not $expected) { throw "Could not find checksum entry for $archive" }
            $actual = (Get-FileHash -LiteralPath $archivePath -Algorithm SHA256).Hash
            # -ne ignores case, and Get-FileHash returns upper-case hex.
            if ($actual -ne $expected) { throw "Checksum mismatch for $archive" }

            $unpacked = Join-Path $tmp 'unpacked'
            if ($onWindows) {
                Expand-Archive -LiteralPath $archivePath -DestinationPath $unpacked
            } else {
                New-Item -ItemType Directory -Path $unpacked | Out-Null
                tar -xzf $archivePath -C $unpacked
                if ($LASTEXITCODE -ne 0) { throw "Could not unpack $archive" }
            }

            New-Item -ItemType Directory -Force -Path $Dir | Out-Null
            $Dir = (Resolve-Path -LiteralPath $Dir).ProviderPath
            Write-Host "==> Installing to $Dir"
            $binary = Join-Path $Dir $exe
            Move-Item -Force -LiteralPath (Join-Path $unpacked $exe) -Destination $binary
            return $binary
        } finally {
            Remove-Item -Recurse -Force -LiteralPath $tmp -ErrorAction SilentlyContinue
        }
    }

    # Invoke-Scoop runs scoop with the default error preference, which Scoop
    # expects, and sends its output to the host so that a caller's return
    # value holds only what the caller returns.
    function Invoke-Scoop {
        $ErrorActionPreference = 'Continue'
        scoop @args | Out-Host
        if ($LASTEXITCODE -ne 0) { throw "scoop $args failed." }
    }

    function Install-WithScoop([string]$Shims) {
        Write-Host '==> Scoop detected - installing via scoop...'
        # The add fails when the bucket is already there, which is fine.
        try { Invoke-Scoop bucket add runware https://github.com/Runware/scoop-bucket.git } catch {}
        Invoke-Scoop install runware
        return Join-Path $Shims $exe
    }

    # Add-UserPath puts $Dir first in the user PATH. It edits the registry
    # value directly, because [Environment]::SetEnvironmentVariable would
    # expand entries such as %USERPROFILE%\bin and store the value as a plain
    # string.
    function Add-UserPath([string]$Dir) {
        $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
        try {
            $raw = $key.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
            $entries = @($raw -split ';' | Where-Object { $_ })
            if ($entries -contains $Dir) { return }
            $key.SetValue('Path', ((@($Dir) + $entries) -join ';'), [Microsoft.Win32.RegistryValueKind]::ExpandString)
        } finally {
            $key.Close()
        }
        # Setting a user variable broadcasts the change, so that new
        # terminals see the new PATH.
        $marker = 'RUNWARE_INSTALL_PATH_CHANGED'
        [Environment]::SetEnvironmentVariable($marker, '1', 'User')
        [Environment]::SetEnvironmentVariable($marker, $null, 'User')
    }

    $downloaded = $false
    $existing = Find-Runware
    $shims = Get-ScoopShims

    if ($existing -and (Test-Init $existing)) {
        Write-Host "==> Using the installed CLI at $existing"
        $runware = $existing
    } elseif ($existing) {
        Write-Host "==> The CLI at $existing is too old: it has no 'serverless init'. Installing the latest version."
        if ($shims -and ((Split-Path -Parent $existing) -eq $shims)) {
            Write-Host '==> Updating via scoop...'
            Invoke-Scoop update
            Invoke-Scoop update runware
            $runware = $existing
        } else {
            $runware = Install-FromRelease $installDir
            $downloaded = $true
        }
    } elseif ($shims) {
        $runware = Install-WithScoop $shims
    } else {
        $runware = Install-FromRelease $installDir
        $downloaded = $true
    }

    # A release from before init would print help for 'serverless init <dir>'
    # and exit 0, so check before delegating to it.
    if (-not (Test-Init $runware)) {
        throw "The CLI at $runware has no 'serverless init', even after the install. The latest release may not include init yet."
    }

    Write-Host "==> Creating the App directory $appDir..."
    # Keep going after a failed init, so that the PATH fix still prints. The
    # script fails with init's status at the end.
    & $runware serverless init $appDir
    $initStatus = $LASTEXITCODE

    if ($downloaded) {
        $dir = Split-Path -Parent $runware
        $current = Find-Runware
        $onPath = ($env:PATH -split [IO.Path]::PathSeparator) -contains $dir
        if ($onPath) {
            if ($current -ne $runware) {
                Write-Host "==> Another CLI at $current comes before $dir in your PATH."
                Write-Host "    Remove it, so that 'runware' runs the version installed now."
            }
        } elseif ($onWindows) {
            Add-UserPath $dir
            $env:PATH = "$dir;$env:PATH"
            Write-Host "==> Installed to $runware"
            Write-Host "==> Added $dir to your user PATH. Open a new terminal to use 'runware' there."
        } else {
            Write-Host "==> Installed to $runware"
            Write-Host "==> $dir is not in your PATH."
            Write-Host ''
            Write-Host 'To use the CLI from anywhere, add this line to your ~/.bashrc, ~/.zshrc, or ~/.profile:'
            Write-Host ''
            Write-Host "    export PATH=`"${dir}:`$PATH`""
            Write-Host ''
            Write-Host 'Then restart your terminal or run: source ~/.bashrc (or your respective config file).'
        }
    }

    if ($initStatus -ne 0) {
        throw "runware serverless init exited with status $initStatus."
    }
}
