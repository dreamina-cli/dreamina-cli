param(
  [string[]]$Targets = @(
    "darwin/arm64",
    "darwin/amd64",
    "linux/amd64",
    "linux/arm64",
    "windows/amd64",
    "windows/arm64"
  )
)

$ErrorActionPreference = "Stop"

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ProjectDir = Split-Path -Parent $ScriptDir
$RepoRoot = Resolve-Path (Join-Path $ProjectDir "..\..\..\..")
$ModuleDir = Join-Path $ProjectDir "src"
$DistDir = Join-Path $ProjectDir "dist"
$PackageDir = Join-Path $DistDir "packages"

if (-not $env:VERSION) {
  try {
    $env:VERSION = (git -C $RepoRoot describe --always --dirty).Trim()
  } catch {
    $env:VERSION = [DateTime]::UtcNow.ToString("yyyyMMddHHmmss")
  }
}

if (-not $env:COMMIT) {
  try {
    $env:COMMIT = (git -C $RepoRoot rev-parse --short HEAD).Trim()
  } catch {
    $env:COMMIT = "unknown"
  }
}

if (-not $env:BUILD_TIME) {
  $env:BUILD_TIME = [DateTime]::UtcNow.ToString("yyyy-MM-ddTHH:mm:ssZ")
}

New-Item -ItemType Directory -Force -Path $DistDir | Out-Null
New-Item -ItemType Directory -Force -Path $PackageDir | Out-Null
$ChecksumFile = Join-Path $PackageDir "SHA256SUMS.txt"
if (Test-Path $ChecksumFile) {
  Remove-Item $ChecksumFile -Force
}

function Add-PackageChecksum {
  param(
    [string]$ArchivePath
  )

  $Hash = (Get-FileHash -Algorithm SHA256 -Path $ArchivePath).Hash.ToLowerInvariant()
  "$Hash  $(Split-Path -Leaf $ArchivePath)" | Out-File -FilePath $ChecksumFile -Encoding ascii -Append
}

foreach ($Target in $Targets) {
  $Parts = $Target.Split("/")
  if ($Parts.Count -ne 2) {
    throw "invalid target: $Target"
  }

  $Goos = $Parts[0]
  $Goarch = $Parts[1]
  $OutDir = Join-Path $DistDir "$Goos-$Goarch"
  $OutBin = Join-Path $OutDir "dreamina"
  $ArchiveBase = "dreamina-$($env:VERSION)-$Goos-$Goarch"
  $StageDir = Join-Path $PackageDir $ArchiveBase
  if ($Goos -eq "windows") {
    $OutBin = "$OutBin.exe"
  }

  New-Item -ItemType Directory -Force -Path $OutDir | Out-Null
  if (Test-Path $StageDir) {
    Remove-Item $StageDir -Recurse -Force
  }
  New-Item -ItemType Directory -Force -Path $StageDir | Out-Null
  Write-Host "==> building $Goos/$Goarch"

  Push-Location $ModuleDir
  try {
    $env:GOOS = $Goos
    $env:GOARCH = $Goarch
    $env:CGO_ENABLED = "0"
    go build `
      -ldflags "-X code.byted.org/videocut-aigc/dreamina_cli/buildinfo.Version=$env:VERSION -X code.byted.org/videocut-aigc/dreamina_cli/buildinfo.Commit=$env:COMMIT -X code.byted.org/videocut-aigc/dreamina_cli/buildinfo.BuildTime=$env:BUILD_TIME" `
      -o $OutBin `
      .
  } finally {
    Pop-Location
    Remove-Item Env:GOOS -ErrorAction SilentlyContinue
    Remove-Item Env:GOARCH -ErrorAction SilentlyContinue
    Remove-Item Env:CGO_ENABLED -ErrorAction SilentlyContinue
  }

  Copy-Item (Join-Path $ProjectDir "README.md") (Join-Path $StageDir "README.md") -Force
  Copy-Item $OutBin (Join-Path $StageDir (Split-Path -Leaf $OutBin)) -Force

  if ($Goos -eq "windows") {
    $ArchivePath = Join-Path $PackageDir "$ArchiveBase.zip"
    if (Test-Path $ArchivePath) {
      Remove-Item $ArchivePath -Force
    }
    Compress-Archive -Path (Join-Path $StageDir "*") -DestinationPath $ArchivePath -CompressionLevel Optimal
  } else {
    $ArchivePath = Join-Path $PackageDir "$ArchiveBase.tar.gz"
    if (Test-Path $ArchivePath) {
      Remove-Item $ArchivePath -Force
    }
    tar -C $PackageDir -czf $ArchivePath $ArchiveBase
  }

  Add-PackageChecksum -ArchivePath $ArchivePath
  Remove-Item $StageDir -Recurse -Force
}

Write-Host "build completed: $DistDir"
