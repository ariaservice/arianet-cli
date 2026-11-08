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
#
# This file is ASCII on purpose: Persian text is written as \uXXXX escapes so it
# survives Windows PowerShell 5.1 reading the script in the system code page.

& {
    Set-StrictMode -Version 2.0
    $ErrorActionPreference = 'Stop'
    $ProgressPreference = 'SilentlyContinue' # Invoke-WebRequest is very slow with the progress bar on 5.1

    $Repo = 'ariaservice/arianet-cli'
    $Binary = 'arianet.exe'

    $Fa = @{
        Installing  = '\u062f\u0631 \u062d\u0627\u0644 \u0646\u0635\u0628'
        ChecksumOk  = '\u0635\u062d\u062a \u0641\u0627\u06cc\u0644 \u062a\u0623\u06cc\u06cc\u062f \u0634\u062f'
        Installed   = '\u0646\u0635\u0628 \u0634\u062f'
        PathAdded   = '\u0628\u0647 PATH \u06a9\u0627\u0631\u0628\u0631 \u0627\u0636\u0627\u0641\u0647 \u0634\u062f\u061b \u067e\u0646\u062c\u0631\u0647\u200c\u0647\u0627\u06cc \u062c\u062f\u06cc\u062f \u062a\u0631\u0645\u06cc\u0646\u0627\u0644 \u0622\u0646 \u0631\u0627 \u0645\u06cc\u200c\u0634\u0646\u0627\u0633\u0646\u062f'
        Next        = '\u0645\u0631\u062d\u0644\u0647 \u0628\u0639\u062f'
        Configure   = '\u062a\u0648\u06a9\u0646 API \u0631\u0627 \u0627\u0632 \u067e\u0646\u0644 \u0628\u0633\u0627\u0632\u06cc\u062f \u0648 \u0627\u06cc\u0646\u062c\u0627 \u0648\u0627\u0631\u062f \u06a9\u0646\u06cc\u062f'
        List        = '\u0641\u0647\u0631\u0633\u062a \u0633\u0631\u0648\u0631\u0647\u0627\u06cc \u0634\u0645\u0627'
        Upgrade     = '\u0628\u0647\u200c\u0631\u0648\u0632\u0631\u0633\u0627\u0646\u06cc'
        Error       = '\u062e\u0637\u0627'
        ArmEmulated = '\u0648\u06cc\u0646\u062f\u0648\u0632 ARM64: \u0646\u0633\u062e\u0647 x64 \u0628\u0627 \u0634\u0628\u06cc\u0647\u200c\u0633\u0627\u0632\u06cc \u0648\u06cc\u0646\u062f\u0648\u0632 \u06f1\u06f1 \u0646\u0635\u0628 \u0645\u06cc\u200c\u0634\u0648\u062f'
        ArmTooOld   = '\u0648\u06cc\u0646\u062f\u0648\u0632 ARM64 \u067e\u06cc\u0634 \u0627\u0632 \u0648\u06cc\u0646\u062f\u0648\u0632 \u06f1\u06f1 \u0628\u0631\u0646\u0627\u0645\u0647\u200c\u0647\u0627\u06cc x64 \u0631\u0627 \u0627\u062c\u0631\u0627 \u0646\u0645\u06cc\u200c\u06a9\u0646\u062f'
        X86         = '\u0648\u06cc\u0646\u062f\u0648\u0632 \u06f3\u06f2 \u0628\u06cc\u062a\u06cc \u067e\u0634\u062a\u06cc\u0628\u0627\u0646\u06cc \u0646\u0645\u06cc\u200c\u0634\u0648\u062f'
        BadChecksum = '\u0641\u0627\u06cc\u0644 \u062f\u0627\u0646\u0644\u0648\u062f\u0634\u062f\u0647 \u0628\u0627 checksums.txt \u06cc\u06a9\u0633\u0627\u0646 \u0646\u06cc\u0633\u062a\u061b \u0646\u0635\u0628 \u0627\u0646\u062c\u0627\u0645 \u0646\u0634\u062f'
        Network     = '\u062f\u0627\u0646\u0644\u0648\u062f \u0646\u0627\u0645\u0648\u0641\u0642 \u0628\u0648\u062f\u061b \u0627\u062a\u0635\u0627\u0644 \u0627\u06cc\u0646\u062a\u0631\u0646\u062a \u0631\u0627 \u0628\u0631\u0631\u0633\u06cc \u06a9\u0646\u06cc\u062f'
    }
    foreach ($key in @($Fa.Keys)) { $Fa[$key] = [regex]::Unescape($Fa[$key]) }

    # A failure carries an English and a Persian line; the catch at the bottom prints both.
    function Fail([string]$English, [string]$Persian) {
        $err = New-Object System.Exception $English
        $err.Data['fa'] = $Persian
        throw $err
    }

    function Say([string]$English, [string]$Persian) {
        if ($Persian) { Write-Host "$English  |  $Persian" } else { Write-Host $English }
    }

    function Download([string]$Url, [string]$OutFile) {
        try {
            Invoke-WebRequest -Uri $Url -OutFile $OutFile -UseBasicParsing -Headers @{ 'User-Agent' = 'arianet-install-ps1' }
        } catch {
            Fail "could not download ${Url}: $($_.Exception.Message)" $Fa.Network
        }
    }

    # Windows PowerShell 5.1 has no $IsWindows and is Windows only.
    $onWindows = ($PSVersionTable.PSEdition -eq 'Desktop') -or ((Get-Variable -Name IsWindows -ValueOnly -ErrorAction SilentlyContinue) -eq $true)

    $savedProtocol = [Net.ServicePointManager]::SecurityProtocol
    $savedOutputEncoding = $null
    $tmpDir = $null
    try {
        # PowerShell 5.1 defaults to TLS 1.0, which GitHub refuses.
        [Net.ServicePointManager]::SecurityProtocol = $savedProtocol -bor [Net.SecurityProtocolType]::Tls12
        if ($onWindows) {
            try {
                $savedOutputEncoding = [Console]::OutputEncoding
                [Console]::OutputEncoding = [Text.Encoding]::UTF8
            } catch { $savedOutputEncoding = $null }
        }

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
                        Say 'Windows on ARM64: installing the x64 build, which Windows 11 runs through emulation.' $Fa.ArmEmulated
                    } else {
                        Fail 'Windows on ARM64 runs x64 programs only from Windows 11 on; there is no native ARM64 build yet.' $Fa.ArmTooOld
                    }
                    break
                }
                default { Fail "unsupported Windows architecture: $osArch (only 64-bit Windows is supported)" $Fa.X86 }
            }
        }

        $installDir = $env:ARIANET_INSTALL_DIR
        if (-not $installDir) {
            if (-not $env:LOCALAPPDATA) { Fail 'LOCALAPPDATA is not set; set ARIANET_INSTALL_DIR to choose the install folder' $null }
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
                    Fail "could not look up the latest release: $($_.Exception.Message)" $Fa.Network
                }
            }
        }
        $version = ([string]$version).Trim()
        if ($version.StartsWith('v')) { $version = $version.Substring(1) }
        if ($version -notmatch '^[0-9]+\.[0-9]+\.[0-9]+([-.][0-9A-Za-z.-]+)?$') {
            Fail "could not determine a valid version (got '$version'); set ARIANET_VERSION, e.g. 0.5.0" $null
        }

        $archive = "arianet_${version}_windows_${arch}.zip"
        $base = "https://github.com/$Repo/releases/download/v$version"

        $tmpDir = Join-Path ([IO.Path]::GetTempPath()) ('arianet-install-' + [Guid]::NewGuid().ToString('N'))
        New-Item -ItemType Directory -Path $tmpDir | Out-Null

        Say "Installing arianet $version (windows/$arch)" "$($Fa.Installing) arianet $version"
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
        if (-not $expected) { Fail "$archive is not listed in checksums.txt" $Fa.BadChecksum }
        $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $archivePath).Hash.ToLowerInvariant()
        if ($actual -ne $expected) { Fail "checksum mismatch for $archive; refusing to install" $Fa.BadChecksum }
        Say '  checksum OK' $Fa.ChecksumOk

        $extractDir = Join-Path $tmpDir 'extract'
        Expand-Archive -LiteralPath $archivePath -DestinationPath $extractDir -Force
        $newExe = Join-Path $extractDir $Binary
        if (-not (Test-Path -LiteralPath $newExe -PathType Leaf)) { Fail "$Binary not found in $archive" $null }

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
            Fail "could not write $target ($($_.Exception.Message)); close any running arianet and try again" $null
        }
        Say "Installed $target" "$($Fa.Installed): $target"

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
                Say "  added $installDir to your user PATH (new terminal windows will find arianet)" $Fa.PathAdded
            }
            $sessionEntries = @($env:Path -split ';' | ForEach-Object { $_.TrimEnd('\') })
            if ($sessionEntries -notcontains $normalized) { $env:Path = "$installDir;$env:Path" }

            Write-Host ''
            & $target version
        } else {
            Write-Host '  (not Windows: PATH left unchanged and the binary was not run)'
        }

        Write-Host ''
        Say 'Next:' "$($Fa.Next):"
        Say '  arianet configure     (paste an API token from https://cloud.ariaservice.net/users/api-tokens)' $Fa.Configure
        Say '  arianet vps list' $Fa.List
        Say '  arianet upgrade       (later, to update)' $Fa.Upgrade
    } catch {
        $message = $_.Exception.Message
        $persian = $null
        if ($_.Exception.Data.Contains('fa')) { $persian = $_.Exception.Data['fa'] }
        Write-Host "error: $message" -ForegroundColor Red
        if ($persian) { Write-Host "$($Fa.Error): $persian" -ForegroundColor Red }
        # Under `irm | iex` an `exit` would close the user's window; a non-terminating
        # error lets scripts and CI see the failure ($?) while the window stays open.
        Write-Error -Message "arianet install failed: $message" -ErrorAction Continue
        $global:LASTEXITCODE = 1
    } finally {
        if ($tmpDir) { Remove-Item -LiteralPath $tmpDir -Recurse -Force -ErrorAction SilentlyContinue }
        if ($null -ne $savedOutputEncoding) { try { [Console]::OutputEncoding = $savedOutputEncoding } catch { } }
        [Net.ServicePointManager]::SecurityProtocol = $savedProtocol
    }
}
