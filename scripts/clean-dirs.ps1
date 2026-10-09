# Remove build directories. Kept as a script because a foreach over a list of
# paths needs a PowerShell variable, and `$` does not survive a Makefile recipe
# (the recipe runs under sh, which expands `$$` to the shell PID).
# Dirs takes a semicolon-separated list: PowerShell binds each bare argument to
# a separate element of $Dirs, which turns "bin,a,b" into one unusable name.
param(
    [Parameter(Mandatory = $true)][string]$Dirs
)

foreach ($d in $Dirs.Split(';')) {
    $path = $d.Trim()
    if (-not $path) {
        continue
    }
    if (Test-Path -LiteralPath $path) {
        Remove-Item -LiteralPath $path -Recurse -Force
        Write-Host "removed $path"
    }
}