# Arianet CLI installer (Windows, PowerShell 5.1 and 7).
#
#   irm https://raw.githubusercontent.com/ariaservice/arianet-cli/main/scripts/install.ps1 | iex
#
# What it does, and nothing else:
#   1. downloads the windows_amd64 release zip and checksums.txt from GitHub Releases,
#   2. verifies the zip's SHA-256 against checksums.txt,
#   3. copies the single arianet.exe into %LOCALAPPDATA%\Programs\arianet (no admin needed),
#   4. adds that folder to your user PATH (once) and to the current window.
# It does not need Administrator rights, change system settings or install anything else.
# `arianet upgrade` later replaces arianet.exe in the same folder.
#
# Environment (optional):
#   ARIANET_VERSION      a release to install, e.g. 0.5.0 (default: the latest release)
#   ARIANET_INSTALL_DIR  where arianet.exe goes (default: %LOCALAPPDATA%\Programs\arianet)
#   GITHUB_TOKEN         used only for the release lookup, to avoid GitHub API rate limits

& {
    Set-StrictMode -Version 2.0
    $ErrorActionPreference = 'Stop'
    $ProgressPreference = 'SilentlyContinue' # Invoke-WebRequest is very slow with the progress bar on 5.1

    $Repo = 'ariaservice/arianet-cli'
    $Binary = 'arianet.exe'

    function Fail([string]$Message) {
        throw (New-Object System.Exception $Message)
    }

    function Say([string]$Message) {
        Write-Host $Message
    }

    function Download([string]$Url, [string]$OutFile) {
        try {
            Invoke-WebRequest -Uri $Url -OutFile $OutFile -UseBasicParsing -Headers @{ 'User-Agent' = 'arianet-install-ps1' }
        } catch {
            # A 404 means the release or the asset does not exist (usually a wrong
            # ARIANET_VERSION), not a network problem. 5.1 throws WebException, 7 throws
            # HttpResponseException; both carry .Response.StatusCode.
            $status = 0
            $response = $null
            if ($_.Exception.PSObject.Properties['Response']) { $response = $_.Exception.Response }
            if ($response -and $response.PSObject.Properties['StatusCode']) {
                try { $status = [int]$response.StatusCode } catch { $status = 0 }
            }
            if ($status -eq 404) {
                Fail "not found: $Url (no release v$version, or it has no $([IO.Path]::GetFileName($OutFile)); check ARIANET_VERSION)"
            }
            Fail "could not download ${Url}: $($_.Exception.Message) (check your internet connection)"
        }
    }

    # Windows PowerShell 5.1 has no $IsWindows and is Windows only.
    $onWindows = ($PSVersionTable.PSEdition -eq 'Desktop') -or ((Get-Variable -Name IsWindows -ValueOnly -ErrorAction SilentlyContinue) -eq $true)

    $savedProtocol = [Net.ServicePointManager]::SecurityProtocol
    $tmpDir = $null
    try {
        # PowerShell 5.1 defaults to TLS 1.0, which GitHub refuses.
        [Net.ServicePointManager]::SecurityProtocol = $savedProtocol -bor [Net.SecurityProtocolType]::Tls12

        # Only windows_amd64 is released (.goreleaser.yaml ignores windows/arm64).
        $arch = 'amd64'
        if ($onWindows) {
            $osArch = $null
            try { $osArch = [string][System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture } catch { $osArch = $null }
            if (-not $osArch) {
                $osArch = $env:PROCESSOR_ARCHITEW6432
                if (-not $osArch) { $osArch = $env:PROCESSOR_ARCHITECTURE }
            }
            switch -Regex ($osArch) {
                '^(X64|AMD64)$' { break }
                '^ARM64$' {
                    if ([Environment]::OSVersion.Version.Build -ge 22000) {
                        Say 'Windows on ARM64: installing the x64 build, which Windows 11 runs through emulation.'
                    } else {
                        Fail 'Windows on ARM64 runs x64 programs only from Windows 11 on; there is no native ARM64 build yet.'
                    }
                    break
                }
                default { Fail "unsupported Windows architecture: $osArch (only 64-bit Windows is supported)" }
            }
        }

        $installDir = $env:ARIANET_INSTALL_DIR
        if (-not $installDir) {
            if (-not $env:LOCALAPPDATA) { Fail 'LOCALAPPDATA is not set; set ARIANET_INSTALL_DIR to choose the install folder' }
            $installDir = Join-Path (Join-Path $env:LOCALAPPDATA 'Programs') 'arianet'
        }
        $installDir = [IO.Path]::GetFullPath($installDir)

        $version = $env:ARIANET_VERSION
        if (-not $version) {
            $headers = @{ 'Accept' = 'application/vnd.github+json'; 'User-Agent' = 'arianet-install-ps1' }
            if ($env:GITHUB_TOKEN) { $headers['Authorization'] = "Bearer $($env:GITHUB_TOKEN)" }
            try {
                $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest" -Headers $headers -UseBasicParsing
                $version = [string]$release.tag_name
            } catch {
                # The API allows 60 anonymous requests an hour per IP; the web redirect has no such limit.
                try {
                    $resp = Invoke-WebRequest -Uri "https://github.com/$Repo/releases/latest" -UseBasicParsing -Headers @{ 'User-Agent' = 'arianet-install-ps1' }
                    $final = $null
                    if ($resp.BaseResponse.PSObject.Properties['ResponseUri']) { $final = $resp.BaseResponse.ResponseUri }
                    elseif ($resp.BaseResponse.PSObject.Properties['RequestMessage']) { $final = $resp.BaseResponse.RequestMessage.RequestUri }
                    if ($final) { $version = ([string]$final).TrimEnd('/').Split('/')[-1] }
                } catch {
                    Fail "could not look up the latest release: $($_.Exception.Message) (check your internet connection)"
                }
            }
        }
        $version = ([string]$version).Trim()
        if ($version.StartsWith('v')) { $version = $version.Substring(1) }
        if ($version -notmatch '^[0-9]+\.[0-9]+\.[0-9]+([-.][0-9A-Za-z.-]+)?$') {
            Fail "could not determine a valid version (got '$version'); set ARIANET_VERSION, e.g. 0.5.0"
        }

        $archive = "arianet_${version}_windows_${arch}.zip"
        $base = "https://github.com/$Repo/releases/download/v$version"

        $tmpDir = Join-Path ([IO.Path]::GetTempPath()) ('arianet-install-' + [Guid]::NewGuid().ToString('N'))
        New-Item -ItemType Directory -Path $tmpDir | Out-Null

        Say "Installing arianet $version (windows/$arch)"
        $archivePath = Join-Path $tmpDir $archive
        $sumsPath = Join-Path $tmpDir 'checksums.txt'
        Download "$base/$archive" $archivePath
        Download "$base/checksums.txt" $sumsPath

        # checksums.txt lines are "<sha256>  <file name>".
        $expected = $null
        foreach ($line in [IO.File]::ReadAllLines($sumsPath)) {
            $fields = $line.Trim() -split '\s+'
            if ($fields.Count -eq 2 -and $fields[1].TrimStart('*') -eq $archive) { $expected = $fields[0].ToLowerInvariant() }
        }
        if (-not $expected) { Fail "$archive is not listed in checksums.txt" }
        $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $archivePath).Hash.ToLowerInvariant()
        if ($actual -ne $expected) { Fail "checksum mismatch for $archive; refusing to install" }
        Say '  checksum OK'

        $extractDir = Join-Path $tmpDir 'extract'
        Expand-Archive -LiteralPath $archivePath -DestinationPath $extractDir -Force
        $newExe = Join-Path $extractDir $Binary
        if (-not (Test-Path -LiteralPath $newExe -PathType Leaf)) { Fail "$Binary not found in $archive" }

        # Copy next to the target under a temporary name, then rename it into place, so a
        # failure never leaves a half-written arianet.exe. A running arianet.exe cannot be
        # overwritten on Windows, so it is moved aside to arianet.exe.old first (the same
        # scheme `arianet upgrade` uses; the CLI deletes the .old file on its next run).
        New-Item -ItemType Directory -Path $installDir -Force | Out-Null
        $target = Join-Path $installDir $Binary
        $staged = Join-Path $installDir ('.arianet-install-' + [Guid]::NewGuid().ToString('N') + '.exe')
        $old = "$target.old"
        try {
            Copy-Item -LiteralPath $newExe -Destination $staged -Force
            if (Test-Path -LiteralPath $target) {
                Remove-Item -LiteralPath $old -Force -ErrorAction SilentlyContinue
                Move-Item -LiteralPath $target -Destination $old -Force
                try {
                    Move-Item -LiteralPath $staged -Destination $target
                } catch {
                    Move-Item -LiteralPath $old -Destination $target -Force
                    throw
                }
                Remove-Item -LiteralPath $old -Force -ErrorAction SilentlyContinue
            } else {
                Move-Item -LiteralPath $staged -Destination $target
            }
        } catch {
            Remove-Item -LiteralPath $staged -Force -ErrorAction SilentlyContinue
            Fail "could not write $target ($($_.Exception.Message)); close any running arianet and try again"
        }
        Say "Installed $target"

        if ($onWindows) {
            # User PATH, read without expanding %VARS% so existing entries keep their form.
            $key = Get-Item -LiteralPath 'HKCU:\Environment'
            $userPath = [string]$key.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
            $entries = @($userPath -split ';' | Where-Object { $_ -ne '' })
            $normalized = $installDir.TrimEnd('\')
            $present = $false
            foreach ($entry in $entries) {
                if ([Environment]::ExpandEnvironmentVariables($entry).TrimEnd('\') -ieq $normalized) { $present = $true }
            }
            if (-not $present) {
                $newPath = (@($entries) + $installDir) -join ';'
                New-ItemProperty -LiteralPath 'HKCU:\Environment' -Name 'Path' -Value $newPath -PropertyType ExpandString -Force | Out-Null
                # Setting any user variable through .NET broadcasts WM_SETTINGCHANGE, so
                # Explorer and new terminals pick up the new PATH without a sign-out.
                [Environment]::SetEnvironmentVariable('ARIANET_INSTALL_TMP', '1', 'User')
                [Environment]::SetEnvironmentVariable('ARIANET_INSTALL_TMP', $null, 'User')
                Say "  added $installDir to your user PATH (new terminal windows will find arianet)"
            }
            $sessionEntries = @($env:Path -split ';' | ForEach-Object { $_.TrimEnd('\') })
            if ($sessionEntries -notcontains $normalized) { $env:Path = "$installDir;$env:Path" }

            Write-Host ''
            & $target version
        } else {
            Write-Host '  (not Windows: PATH left unchanged and the binary was not run)'
        }

        Write-Host ''
        Say 'Next:'
        Say '  arianet configure     (paste an API token from https://cloud.ariaservice.net/users/api-tokens)'
        Say '  arianet vps list'
        Say '  arianet upgrade       (later, to update)'
    } catch {
        $message = $_.Exception.Message
        Write-Host "error: $message" -ForegroundColor Red
        # Under `irm | iex` an `exit` would close the user's window; a non-terminating
        # error lets scripts and CI see the failure ($?) while the window stays open.
        Write-Error -Message "arianet install failed: $message" -ErrorAction Continue
        $global:LASTEXITCODE = 1
    } finally {
        if ($tmpDir) { Remove-Item -LiteralPath $tmpDir -Recurse -Force -ErrorAction SilentlyContinue }
        [Net.ServicePointManager]::SecurityProtocol = $savedProtocol
    }
}
