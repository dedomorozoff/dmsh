# Pack the contents of a directory into a zip file.
#
# Kept as a script because Compress-Archive needs the Microsoft.PowerShell.Archive
# module, which is not always loadable, and `tar -a` needs Windows 10 1803+.
# Both are tried, and the destination is verified afterwards: a packaging step
# that silently produces no file is worse than one that fails.
param(
    [Parameter(Mandatory = $true)][string]$Source,
    [Parameter(Mandatory = $true)][string]$Destination
)

$ErrorActionPreference = 'Stop'
$zip = Join-Path (Get-Location) $Destination

if (-not (Test-Path $Source)) {
    Write-Error "source directory '$Source' does not exist"
    exit 1
}

if (Test-Path $zip) {
    Remove-Item $zip -Force
}

# Compress-Archive omits dotfiles on some PowerShell versions; tar -a does not.
$packed = $false
try {
    Compress-Archive -Path (Join-Path $Source '*') -DestinationPath $zip -Force
    $packed = Test-Path $zip
} catch {
    Write-Host "Compress-Archive failed: $($_.Exception.Message)"
    Write-Host 'Falling back to tar -a.'
}

if (-not $packed) {
    tar -a -c -f $Destination -C $Source .
    $packed = Test-Path $zip
}

if (-not $packed) {
    Write-Error "could not create '$Destination'"
    exit 1
}

$count = (Get-ChildItem $Source -Recurse -File).Count
Write-Host "Created $Destination ($count file(s))."