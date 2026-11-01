# Runs the Go checks (gofmt, go vet, go test) in a Linux container on a COPY of the sources.
# A bind mount from Windows does not behave like a Linux filesystem (links, modes), so the tests run on the container's own disk.
# Usage: powershell -File tools\test-in-docker.ps1 [-Packages ./internal/library/...]
param([string]$Packages = './...')
$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent $PSScriptRoot
$name = 'bootlace-gate-' + [guid]::NewGuid().ToString('N').Substring(0, 8)
$script = "! gofmt -l cmd internal | grep . && go vet $Packages && go test $Packages"
try {
    docker create --name $name -w /src golang:latest sh -c $script | Out-Null
    docker cp "$repo/." "${name}:/src"
    docker start -a $name
    $code = $LASTEXITCODE
} finally {
    docker rm -f $name | Out-Null
}
exit $code
