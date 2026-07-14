param(
    [Parameter(Mandatory = $true)]
    [string]$ReleaseId,
    [string]$OutputDirectory = "release-artifacts"
)

$ErrorActionPreference = "Stop"
$repoRoot = (git rev-parse --show-toplevel).Trim()
if (-not $repoRoot) {
    throw "Run this script inside the Git repository."
}

$dirty = git -C $repoRoot status --porcelain
if ($dirty) {
    throw "The worktree is not clean. Commit the reviewed release changes before packaging."
}

$commit = (git -C $repoRoot rev-parse HEAD).Trim()
$timestamp = Get-Date -Format "yyyyMMdd-HHmmss"
$outputRoot = Join-Path $repoRoot $OutputDirectory
$stagingRoot = Join-Path $env:TEMP "new-api-$ReleaseId-$timestamp"
$sourceRoot = Join-Path $stagingRoot "source"
$archive = Join-Path $outputRoot "$ReleaseId-$($commit.Substring(0, 12)).tar.gz"
$manifest = Join-Path $outputRoot "$ReleaseId-$($commit.Substring(0, 12)).manifest.txt"

New-Item -ItemType Directory -Force -Path $outputRoot, $sourceRoot | Out-Null
try {
    $gitArchive = Join-Path $stagingRoot "source.tar"
    git -C $repoRoot archive --format=tar --output=$gitArchive HEAD
    if ($LASTEXITCODE -ne 0) { throw "git archive failed" }
    tar -xf $gitArchive -C $sourceRoot
    if ($LASTEXITCODE -ne 0) { throw "extract source archive failed" }

    Set-Content -LiteralPath (Join-Path $sourceRoot "VERSION") -Value $ReleaseId -NoNewline -Encoding ascii
    @(
        "release_id=$ReleaseId"
        "commit=$commit"
        "created_at=$((Get-Date).ToString('o'))"
    ) | Set-Content -LiteralPath (Join-Path $sourceRoot "RELEASE-METADATA") -Encoding ascii
    tar -czf $archive -C $sourceRoot .
    if ($LASTEXITCODE -ne 0) { throw "create release archive failed" }

    $hash = (Get-FileHash -Algorithm SHA256 -LiteralPath $archive).Hash.ToLowerInvariant()
    @(
        "release_id=$ReleaseId"
        "commit=$commit"
        "created_at=$((Get-Date).ToString('o'))"
        "archive=$([IO.Path]::GetFileName($archive))"
        "sha256=$hash"
    ) | Set-Content -LiteralPath $manifest -Encoding ascii

    Write-Host "Release archive: $archive"
    Write-Host "Manifest: $manifest"
    Write-Host "SHA256: $hash"
}
finally {
    if (Test-Path -LiteralPath $stagingRoot) {
        Remove-Item -LiteralPath $stagingRoot -Recurse -Force
    }
}
