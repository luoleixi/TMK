[CmdletBinding()]
param(
    [string]$Version = "",
    [string]$OutputDirectory = ""
)

$ErrorActionPreference = "Stop"
$repoRoot = Split-Path -Parent $PSScriptRoot
$clientRoot = Join-Path $repoRoot "TMK-Client"
$backendConfigPath = Join-Path $clientRoot "backend_config.go"
$managedEnvironmentVariables = @(
    "TMK_ENV",
    "APP_ENV",
    "GO_ENV",
    "TMK_BACKEND_URL",
    "GOCACHE",
    "GOMODCACHE",
    "GOTMPDIR"
)
$originalEnvironment = @{}

foreach ($name in $managedEnvironmentVariables) {
    $value = [Environment]::GetEnvironmentVariable($name, "Process")
    $originalEnvironment[$name] = $value
}

try {
    $env:TMK_ENV = "production"
    Remove-Item Env:APP_ENV -ErrorAction SilentlyContinue
    Remove-Item Env:GO_ENV -ErrorAction SilentlyContinue
    Remove-Item Env:TMK_BACKEND_URL -ErrorAction SilentlyContinue

    $goBuildRoot = Join-Path $clientRoot "bin\.go-build"
    $env:GOCACHE = Join-Path $goBuildRoot "cache"
    $env:GOMODCACHE = Join-Path $goBuildRoot "modules"
    $env:GOTMPDIR = Join-Path $goBuildRoot "tmp"
    New-Item -ItemType Directory -Force $env:GOCACHE, $env:GOMODCACHE, $env:GOTMPDIR | Out-Null

    $source = Get-Content -Raw -Encoding utf8 $backendConfigPath
    $match = [regex]::Match($source, 'productionBackendBaseURL\s*=\s*"([^"]+)"')
    if (-not $match.Success) {
        throw "Cannot find productionBackendBaseURL in $backendConfigPath"
    }
    $productionUri = [Uri]$match.Groups[1].Value
    if ($productionUri.Host -in @("localhost", "127.0.0.1", "::1") -or $productionUri.Port -eq 18080) {
        throw "Production backend points to a test or loopback endpoint: $productionUri"
    }

    Push-Location $clientRoot
    try {
        & go test -tags production ./...
        if ($LASTEXITCODE -ne 0) {
            throw "Production client tests failed"
        }

        & wails3 task build
        if ($LASTEXITCODE -ne 0) {
            throw "Production client build failed"
        }
    }
    finally {
        Pop-Location
    }

    $artifact = Join-Path $clientRoot "bin\tmk-client.exe"
    if (-not (Test-Path -LiteralPath $artifact -PathType Leaf)) {
        throw "Expected client artifact was not produced: $artifact"
    }

    if ([string]::IsNullOrWhiteSpace($Version)) {
        $Version = (& git -C $repoRoot describe --tags --always).Trim()
    }
    $commit = (& git -C $repoRoot rev-parse HEAD).Trim()
    $metadata = [ordered]@{
        version = $Version
        commit = $commit
        environment = "production"
        backend = $productionUri.AbsoluteUri.TrimEnd("/")
        built_at_utc = [DateTime]::UtcNow.ToString("o")
        artifact_sha256 = (Get-FileHash -Algorithm SHA256 -LiteralPath $artifact).Hash.ToLowerInvariant()
    }

    if ([string]::IsNullOrWhiteSpace($OutputDirectory)) {
        $OutputDirectory = Join-Path $clientRoot "bin\release"
    }
    New-Item -ItemType Directory -Force -Path $OutputDirectory | Out-Null
    $releaseArtifact = Join-Path $OutputDirectory "TMK-Client-$Version.exe"
    Copy-Item -LiteralPath $artifact -Destination $releaseArtifact -Force
    $metadata | ConvertTo-Json | Set-Content -Encoding utf8 (Join-Path $OutputDirectory "build-metadata.json")

    Write-Host "Production client created: $releaseArtifact"
}
finally {
    foreach ($name in $managedEnvironmentVariables) {
        $value = $originalEnvironment[$name]
        if ($null -eq $value) {
            [Environment]::SetEnvironmentVariable($name, $null, "Process")
        }
        else {
            [Environment]::SetEnvironmentVariable($name, $value, "Process")
        }
    }
}
