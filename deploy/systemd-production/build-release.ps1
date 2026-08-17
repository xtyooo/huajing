[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidatePattern('^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$')]
    [string]$ReleaseId,

    [string]$OutputRoot,

    [switch]$AllowDirty,

    [switch]$SkipTests
)

$ErrorActionPreference = 'Stop'
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
if ([string]::IsNullOrWhiteSpace($OutputRoot)) {
    $OutputRoot = Join-Path $repoRoot 'dist\releases'
}

Push-Location $repoRoot
try {
    $dirty = @(git status --porcelain)
    if ($LASTEXITCODE -ne 0) {
        throw 'git status failed'
    }
    if ($dirty.Count -gt 0 -and -not $AllowDirty) {
        throw 'working tree is dirty; commit the release or pass -AllowDirty for an explicit test deployment'
    }

    if (-not $SkipTests) {
        go test ./...
        if ($LASTEXITCODE -ne 0) {
            throw 'go test ./... failed'
        }
    }

    $gitCommit = (git rev-parse HEAD).Trim()
    $version = (git describe --tags --always --dirty).Trim()
    $outputDir = Join-Path $OutputRoot $ReleaseId
    if (Test-Path -LiteralPath $outputDir) {
        throw "release output already exists: $outputDir"
    }
    New-Item -ItemType Directory -Path $outputDir -Force | Out-Null

    $binaryPath = Join-Path $outputDir 'main'
    $oldCgo = $env:CGO_ENABLED
    $oldGoos = $env:GOOS
    $oldGoarch = $env:GOARCH
    try {
        $env:CGO_ENABLED = '0'
        $env:GOOS = 'linux'
        $env:GOARCH = 'amd64'
        go build -trimpath -ldflags "-s -w -X github.com/QuantumNous/new-api/common.Version=$version" -o $binaryPath .
        if ($LASTEXITCODE -ne 0) {
            throw 'Linux binary build failed'
        }
    }
    finally {
        $env:CGO_ENABLED = $oldCgo
        $env:GOOS = $oldGoos
        $env:GOARCH = $oldGoarch
    }

    $gzipPath = "$binaryPath.gz"
    $inputStream = [System.IO.File]::OpenRead($binaryPath)
    try {
        $outputStream = [System.IO.File]::Create($gzipPath)
        try {
            $gzipStream = [System.IO.Compression.GZipStream]::new(
                $outputStream,
                [System.IO.Compression.CompressionLevel]::Optimal,
                $false
            )
            try {
                $inputStream.CopyTo($gzipStream)
            }
            finally {
                $gzipStream.Dispose()
            }
        }
        finally {
            $outputStream.Dispose()
        }
    }
    finally {
        $inputStream.Dispose()
    }

    $binarySha = (Get-FileHash -Algorithm SHA256 -LiteralPath $binaryPath).Hash.ToLowerInvariant()
    $gzipSha = (Get-FileHash -Algorithm SHA256 -LiteralPath $gzipPath).Hash.ToLowerInvariant()
    $manifest = [ordered]@{
        release_id       = $ReleaseId
        git_commit       = $gitCommit
        version          = $version
        created_at       = [DateTimeOffset]::Now.ToString('o')
        database_mode    = 'code-only'
        binary_sha256    = $binarySha
        gzip_sha256      = $gzipSha
        binary_bytes     = (Get-Item -LiteralPath $binaryPath).Length
        gzip_bytes       = (Get-Item -LiteralPath $gzipPath).Length
    }

    $utf8NoBom = [System.Text.UTF8Encoding]::new($false)
    [System.IO.File]::WriteAllText(
        (Join-Path $outputDir 'manifest.json'),
        ($manifest | ConvertTo-Json),
        $utf8NoBom
    )
    [System.IO.File]::WriteAllText(
        (Join-Path $outputDir 'SHA256SUMS'),
        "$gzipSha  main.gz`n$binarySha  main`n",
        $utf8NoBom
    )

    [pscustomobject]@{
        release_id    = $ReleaseId
        output_dir    = $outputDir
        binary_path   = $binaryPath
        gzip_path     = $gzipPath
        manifest_path = (Join-Path $outputDir 'manifest.json')
        sums_path     = (Join-Path $outputDir 'SHA256SUMS')
        binary_sha256 = $binarySha
        gzip_sha256   = $gzipSha
        binary_bytes  = $manifest.binary_bytes
        gzip_bytes    = $manifest.gzip_bytes
    } | ConvertTo-Json -Compress
}
finally {
    Pop-Location
}
