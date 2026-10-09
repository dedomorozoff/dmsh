# Copy the MinGW runtime DLLs next to the built binary so it runs without a
# MinGW installation on PATH. Kept in a script because the shell quoting needed
# for powershell does not survive a Makefile recipe.
param(
    [string]$MingwBin = $env:MINGW_BIN,
    [string]$Dest = 'bin'
)

$ErrorActionPreference = 'Stop'

$dlls = @('libstdc++-6.dll', 'libgcc_s_seh-1.dll', 'libgomp-1.dll', 'libwinpthread-1.dll')
$copied = 0

if (Test-Path $MingwBin) {
    if (-not (Test-Path $Dest)) {
        New-Item -ItemType Directory -Path $Dest | Out-Null
    }
    foreach ($dll in $dlls) {
        $src = Join-Path $MingwBin $dll
        if (Test-Path $src) {
            Copy-Item $src $Dest -Force
            $copied++
        }
    }
}

if ($copied -eq 0) {
    Write-Host "MinGW DLLs not found in '$MingwBin'. Set MINGW_BIN to your MinGW bin directory." -ForegroundColor Yellow
    Write-Host "The binary was built, but at runtime it may fail with 'libstdc++-6.dll not found'."
} else {
    Write-Host "Copied $copied MinGW runtime DLL(s) to $Dest/."
}