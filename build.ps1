param(
    [string]$Version = "dev",
    [string]$DistDir = ""
)

$ErrorActionPreference = "Stop"

$RootDir = Split-Path -Parent $MyInvocation.MyCommand.Path
if ([string]::IsNullOrWhiteSpace($DistDir)) {
    $DistDir = Join-Path $RootDir "dist"
}

$AppName = "bootstrapctl"
$Platforms = @(
    @{ GOOS = "linux"; GOARCH = "amd64" },
    @{ GOOS = "linux"; GOARCH = "arm64" }
)

Write-Host "[INFO] project: $AppName"
Write-Host "[INFO] version: $Version"
Write-Host "[INFO] dist: $DistDir"

if (Test-Path $DistDir) {
    Remove-Item -Recurse -Force $DistDir
}
New-Item -ItemType Directory -Path $DistDir | Out-Null

$ChecksumLines = @()

foreach ($Platform in $Platforms) {
    $Goos = $Platform.GOOS
    $Goarch = $Platform.GOARCH
    $TargetDir = Join-Path $DistDir "$Goos-$Goarch"
    $BinaryPath = Join-Path $TargetDir $AppName

    New-Item -ItemType Directory -Path $TargetDir -Force | Out-Null

    Write-Host "[INFO] building $Goos/$Goarch"
    $env:CGO_ENABLED = "0"
    $env:GOOS = $Goos
    $env:GOARCH = $Goarch
    go build `
        -trimpath `
        -ldflags "-s -w -X github.com/yuanyp8/bootstrapctl/internal/app.version=$Version" `
        -o $BinaryPath `
        ./cmd/bootstrapctl

    $Hash = (Get-FileHash -Path $BinaryPath -Algorithm SHA256).Hash.ToLowerInvariant()
    $ChecksumLines += "$Hash  $Goos-$Goarch/$AppName"
}

Set-Content -Path (Join-Path $DistDir "checksums.txt") -Value $ChecksumLines
Write-Host "[INFO] build finished"
