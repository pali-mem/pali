param(
    [string]$Version = $env:PALI_VERSION,
    [string]$Repo = $(if ($env:PALI_REPO) { $env:PALI_REPO } else { "pali-mem/pali" }),
    [string]$InstallDir = $(if ($env:PALI_INSTALL_DIR) { $env:PALI_INSTALL_DIR } else { Join-Path $HOME "AppData\Local\Programs\Pali\bin" })
)

$ErrorActionPreference = "Stop"

function Get-ReleaseJson {
    param([string]$Repo, [string]$Version)

    if ([string]::IsNullOrWhiteSpace($Version)) {
        $url = "https://api.github.com/repos/$Repo/releases/latest"
    } else {
        $url = "https://api.github.com/repos/$Repo/releases/tags/$Version"
    }

    Invoke-RestMethod -Uri $url -Headers @{ "User-Agent" = "pali-installer" }
}

function Get-GoArch {
    switch ($env:PROCESSOR_ARCHITECTURE) {
        "AMD64" { return "amd64" }
        "ARM64" { return "arm64" }
        default { throw "Unsupported architecture: $env:PROCESSOR_ARCHITECTURE" }
    }
}

function Ensure-UserPath {
    param([string]$InstallDir)

    function Normalize-PathEntry {
        param([string]$Value)

        if ([string]::IsNullOrWhiteSpace($Value)) {
            return ""
        }

        $expanded = [Environment]::ExpandEnvironmentVariables($Value.Trim())
        try {
            return [IO.Path]::GetFullPath($expanded).TrimEnd('\').ToLowerInvariant()
        }
        catch {
            return $expanded.TrimEnd('\').ToLowerInvariant()
        }
    }

    $currentUserPath = [Environment]::GetEnvironmentVariable("Path", "User")
    $pathParts = @()
    if (-not [string]::IsNullOrWhiteSpace($currentUserPath)) {
        $pathParts = $currentUserPath.Split(';') | Where-Object { -not [string]::IsNullOrWhiteSpace($_) }
    }

    $normalizedInstall = Normalize-PathEntry $InstallDir
    $hasUserPath = $pathParts | Where-Object { (Normalize-PathEntry $_) -eq $normalizedInstall }

    if (-not $hasUserPath) {
        $newPath = if ($currentUserPath) { "$currentUserPath;$InstallDir" } else { $InstallDir }
        [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
    }

    $sessionParts = $env:Path.Split(';') | Where-Object { -not [string]::IsNullOrWhiteSpace($_) }
    $hasSessionPath = $sessionParts | Where-Object { (Normalize-PathEntry $_) -eq $normalizedInstall }
    if (-not $hasSessionPath) {
        $env:Path = "$InstallDir;$env:Path"
    }
}

$release = Get-ReleaseJson -Repo $Repo -Version $Version
$resolvedVersion = $release.tag_name
if ([string]::IsNullOrWhiteSpace($resolvedVersion)) {
    throw "Failed to resolve release version from GitHub API."
}

$goArch = Get-GoArch
$archiveName = "pali_${resolvedVersion}_windows_${goArch}.zip"
$archiveAsset = $release.assets | Where-Object { $_.name -eq $archiveName } | Select-Object -First 1
$checksumAsset = $release.assets | Where-Object { $_.name -eq "SHA256SUMS" } | Select-Object -First 1

if (-not $archiveAsset -or -not $checksumAsset) {
    throw "Release assets for $archiveName are missing."
}

$tempDir = Join-Path ([IO.Path]::GetTempPath()) ("pali-install-" + [guid]::NewGuid().ToString("n"))
New-Item -ItemType Directory -Path $tempDir | Out-Null

try {
    $archivePath = Join-Path $tempDir $archiveName
    $checksumPath = Join-Path $tempDir "SHA256SUMS"
    Invoke-WebRequest -Uri $archiveAsset.browser_download_url -OutFile $archivePath -Headers @{ "User-Agent" = "pali-installer" }
    Invoke-WebRequest -Uri $checksumAsset.browser_download_url -OutFile $checksumPath -Headers @{ "User-Agent" = "pali-installer" }

    $expectedSha = Get-Content $checksumPath |
        ForEach-Object {
            $parts = $_ -split '\s+', 2
            if ($parts.Count -eq 2 -and $parts[1].Trim() -eq $archiveName) {
                $parts[0].Trim()
            }
        } |
        Select-Object -First 1
    if ([string]::IsNullOrWhiteSpace($expectedSha)) {
        throw "Checksum for $archiveName not found."
    }

    $actualSha = (Get-FileHash -Path $archivePath -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actualSha -ne $expectedSha.ToLowerInvariant()) {
        throw "Checksum verification failed for $archiveName."
    }

    New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
    Expand-Archive -Path $archivePath -DestinationPath $tempDir -Force
    Copy-Item -Path (Join-Path $tempDir "pali.exe") -Destination (Join-Path $InstallDir "pali.exe") -Force

    Ensure-UserPath -InstallDir $InstallDir

    Write-Host "Installed pali $resolvedVersion to $(Join-Path $InstallDir 'pali.exe')"
    Write-Host ""
    Write-Host "Next:"
    Write-Host "  pali init"
    Write-Host "  pali serve"
    Write-Host ""
    Write-Host "If a new terminal does not see 'pali' yet, restart PowerShell to reload PATH."
    Write-Host "Docs:"
    Write-Host "  https://pali-mem.github.io/pali/"
}
finally {
    Remove-Item -Path $tempDir -Recurse -Force -ErrorAction SilentlyContinue
}
