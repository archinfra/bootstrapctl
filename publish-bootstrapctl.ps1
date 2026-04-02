param(
    [string]$RemoteName = "bootstrapctl",
    [string]$RemoteUrl = "https://github.com/yuanyp8/bootstrapctl.git",
    [string]$Branch = "main",
    [string]$ExportBranch = "bootstrapctl-export",
    [switch]$SetUpstream,
    [switch]$DryRun
)

$ErrorActionPreference = "Stop"

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$RepoRoot = git -C $ScriptDir rev-parse --show-toplevel
if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($RepoRoot)) {
    throw "Cannot locate outer git repository root. Make sure bootstrapctl is still inside the release monorepo."
}

$Prefix = "src/01-init/bootstrapctl"

Write-Host "== bootstrapctl publish ==" -ForegroundColor Cyan
Write-Host "repo root   : $RepoRoot"
Write-Host "prefix      : $Prefix"
Write-Host "remote name : $RemoteName"
Write-Host "remote url  : $RemoteUrl"
Write-Host "target branch: $Branch"
Write-Host "export branch: $ExportBranch"

$Status = git -C $RepoRoot status --porcelain -- $Prefix
if ($LASTEXITCODE -ne 0) {
    throw "Failed to inspect worktree status for bootstrapctl."
}
if (-not [string]::IsNullOrWhiteSpace($Status)) {
    Write-Host ""
    Write-Host "bootstrapctl has uncommitted changes. Commit them before publish:" -ForegroundColor Yellow
    Write-Host $Status
    throw "Publish stopped because working tree is dirty."
}

$ExistingRemote = @(git -C $RepoRoot remote)
if ($LASTEXITCODE -ne 0) {
    throw "Failed to read git remotes."
}

if ($ExistingRemote -notcontains $RemoteName) {
    if ([string]::IsNullOrWhiteSpace($RemoteUrl)) {
        throw "Remote '$RemoteName' does not exist and no RemoteUrl was provided."
    }
    if (-not $DryRun) {
        git -C $RepoRoot remote add $RemoteName $RemoteUrl
        if ($LASTEXITCODE -ne 0) {
            throw "Failed to add remote '$RemoteName'."
        }
    }
    Write-Host "remote action: add $RemoteName"
} elseif (-not [string]::IsNullOrWhiteSpace($RemoteUrl)) {
    if (-not $DryRun) {
        git -C $RepoRoot remote set-url $RemoteName $RemoteUrl
        if ($LASTEXITCODE -ne 0) {
            throw "Failed to update remote '$RemoteName'."
        }
    }
    Write-Host "remote action: set-url $RemoteName"
}

$BranchExists = git -C $RepoRoot branch --list $ExportBranch
if (-not [string]::IsNullOrWhiteSpace($BranchExists)) {
    if (-not $DryRun) {
        git -C $RepoRoot branch -D $ExportBranch | Out-Null
        if ($LASTEXITCODE -ne 0) {
            throw "Failed to delete old export branch '$ExportBranch'."
        }
    }
    Write-Host "branch action: delete old $ExportBranch"
}

Write-Host "running subtree split..." -ForegroundColor Cyan
if (-not $DryRun) {
    git -C $RepoRoot subtree split --prefix $Prefix -b $ExportBranch
    if ($LASTEXITCODE -ne 0) {
        throw "git subtree split failed."
    }
}

$PushArgs = @("-C", $RepoRoot, "push")
if ($SetUpstream) {
    $PushArgs += "-u"
}
$PushArgs += @($RemoteName, "$ExportBranch`:$Branch")

Write-Host "push command:" -ForegroundColor Cyan
Write-Host ("git " + ($PushArgs -join " "))

if ($DryRun) {
    Write-Host "dry-run mode: push skipped." -ForegroundColor Yellow
    exit 0
}

git @PushArgs
if ($LASTEXITCODE -ne 0) {
    throw "Push failed."
}

Write-Host "publish completed." -ForegroundColor Green
