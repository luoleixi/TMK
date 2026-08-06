[CmdletBinding()]
param(
    [string]$Version = ""
)

$ErrorActionPreference = "Stop"
$repoRoot = Split-Path -Parent $PSScriptRoot
$clientRoot = Join-Path $repoRoot "TMK-Client"
$backendConfigPath = Join-Path $clientRoot "internal\client\runtime\backend_config.go"
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
    $originalEnvironment[$name] = [Environment]::GetEnvironmentVariable($name, "Process")
}

try {
    $env:TMK_ENV = "test"
    Remove-Item Env:APP_ENV -ErrorAction SilentlyContinue
    Remove-Item Env:GO_ENV -ErrorAction SilentlyContinue
    Remove-Item Env:TMK_BACKEND_URL -ErrorAction SilentlyContinue

    $goBuildRoot = Join-Path $clientRoot "bin\.go-build"
    $env:GOCACHE = Join-Path $goBuildRoot "cache"
    $env:GOMODCACHE = Join-Path $goBuildRoot "modules"
    $env:GOTMPDIR = Join-Path $goBuildRoot "tmp"
    New-Item -ItemType Directory -Force $env:GOCACHE, $env:GOMODCACHE, $env:GOTMPDIR | Out-Null

    $source = Get-Content -Raw -Encoding utf8 $backendConfigPath
    $match = [regex]::Match($source, 'testBackendBaseURL\s*=\s*"([^"]+)"')
    if (-not $match.Success) {
        throw "Cannot find testBackendBaseURL in $backendConfigPath"
    }
    $testUri = [Uri]$match.Groups[1].Value
    if ($testUri.Host -in @("localhost", "127.0.0.1", "::1") -or $testUri.AbsoluteUri -notmatch '/tmk-test/?$') {
        throw "Test backend does not point to the shared test endpoint: $testUri"
    }

    Push-Location $clientRoot
    try {
        Push-Location "frontend"
        try {
            & npm ci
            if ($LASTEXITCODE -ne 0) {
                throw "Frontend dependency installation failed"
            }
            & npm run build
            if ($LASTEXITCODE -ne 0) {
                throw "Test frontend build failed"
            }
        }
        finally {
            Pop-Location
        }

        & go test ./...
        if ($LASTEXITCODE -ne 0) {
            throw "Test client tests failed"
        }

        & wails3 task build DEV=true APP_NAME=tmk-client-test
        if ($LASTEXITCODE -ne 0) {
            throw "Test client build failed"
        }
    }
    finally {
        Pop-Location
    }

    $artifact = Join-Path $clientRoot "bin\tmk-client-test.exe"
    if (-not (Test-Path -LiteralPath $artifact -PathType Leaf)) {
        throw "Expected test client artifact was not produced: $artifact"
    }

    if ([string]::IsNullOrWhiteSpace($Version)) {
        $Version = "test-" + (& git -C $repoRoot rev-parse --short HEAD).Trim()
    }
    if ($Version -notmatch '^[A-Za-z0-9._-]+$') {
        throw "Version contains unsupported path characters: $Version"
    }

    $commit = (& git -C $repoRoot rev-parse HEAD).Trim()
    $metadata = [ordered]@{
        version = $Version
        commit = $commit
        environment = "test"
        backend = $testUri.AbsoluteUri.TrimEnd("/")
        built_at_utc = [DateTime]::UtcNow.ToString("o")
        artifact_sha256 = (Get-FileHash -Algorithm SHA256 -LiteralPath $artifact).Hash.ToLowerInvariant()
    }

    $outputDirectory = Join-Path $clientRoot "bin\test\$Version"
    New-Item -ItemType Directory -Force -Path $outputDirectory | Out-Null
    $releaseArtifact = Join-Path $outputDirectory "TMK-Client-Test-$Version.exe"
    Copy-Item -LiteralPath $artifact -Destination $releaseArtifact -Force
    $metadata | ConvertTo-Json | Set-Content -Encoding utf8 (Join-Path $outputDirectory "build-metadata.json")

    Write-Host "Test client created: $releaseArtifact"
}
finally {
    foreach ($name in $managedEnvironmentVariables) {
        $value = $originalEnvironment[$name]
        [Environment]::SetEnvironmentVariable($name, $value, "Process")
    }
}
