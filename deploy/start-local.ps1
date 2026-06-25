Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$deployDir = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $deployDir

if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    throw "Docker was not found. Install Docker Desktop first, then run this script again."
}

$dockerInfo = docker info 2>&1
if ($LASTEXITCODE -ne 0) {
    throw "Docker is installed but the daemon is not running. Start Docker Desktop, then run this script again.`n$dockerInfo"
}

foreach ($dir in "data", "postgres_data", "redis_data") {
    New-Item -ItemType Directory -Force $dir | Out-Null
}

docker compose `
    -f docker-compose.local.yml `
    -f docker-compose.local-build.yml `
    up -d --build

Write-Host ""
Write-Host "LevelUpAPI is starting at http://localhost:8080"
Write-Host "Follow logs with:"
Write-Host "docker compose -f docker-compose.local.yml -f docker-compose.local-build.yml logs -f sub2api"
