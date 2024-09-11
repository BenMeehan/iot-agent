# Makefile equivalent script for powershell

$go = "go"
$buildDir = "bin"
$buildBinary = "$buildDir\agent"
$pkg = ".\..."

function Test-Project {
    Write-Host "Running tests..."
    & $go test $pkg -v
}

function Build-Project {
    Write-Host "Building the project..."
    if (-Not (Test-Path $buildDir)) {
        New-Item -ItemType Directory -Path $buildDir | Out-Null
    }
    & $go build -o $buildBinary .\cmd\agent
}

function Clean-Project {
    Write-Host "Cleaning up..."
    if (Test-Path $buildDir) {
        Remove-Item -Recurse -Force $buildDir
    }
}

# Main script
param(
    [switch]$test,
    [switch]$build,
    [switch]$clean
)

if ($test) {
    Test-Project
}

if ($build) {
    Build-Project
}

if ($clean) {
    Clean-Project
}

if (-not $test -and -not $build -and -not $clean) {
    Test-Project
    Build-Project
}
