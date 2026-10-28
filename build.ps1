# build.ps1 - compila o app, o instalador e o pacote da release
# Uso:  powershell -ExecutionPolicy Bypass -File build.ps1 [-Version 1.0.0]
param([string]$Version = "1.0.0")
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $root

Write-Host "[1/4] compilando batterytray.exe ..."
$env:CGO_ENABLED = "1"
go build -ldflags "-s -w -H windowsgui" -o batterytray.exe .
if ($LASTEXITCODE -ne 0) { throw "go build falhou" }

$csc = Join-Path $env:WINDIR "Microsoft.NET\Framework64\v4.0.30319\csc.exe"
if (-not (Test-Path $csc)) { throw "csc nao encontrado: $csc" }

New-Item -ItemType Directory -Force -Path dist | Out-Null

Write-Host "[2/4] compilando Setup.exe (app embutido como recurso) ..."
& $csc /nologo /target:winexe /optimize+ /platform:anycpu `
    /out:dist\Setup.exe /win32icon:icon.ico `
    /resource:batterytray.exe,BatteryTraySetup.app `
    /r:System.dll /r:System.Drawing.dll /r:System.Windows.Forms.dll `
    installer\setup.cs
if ($LASTEXITCODE -ne 0) { throw "csc falhou" }

Write-Host "[3/4] reunindo artefatos em dist\ ..."
Copy-Item batterytray.exe dist\ -Force
Copy-Item README.md dist\LEIA-ME.md -Force

Write-Host "[4/4] gerando zip da release ..."
$zip = "dist\Bateria-Dispositivos-v$Version.zip"
if (Test-Path $zip) { Remove-Item $zip -Force }
Compress-Archive -Path dist\batterytray.exe, dist\Setup.exe, dist\LEIA-ME.md `
    -DestinationPath $zip -Force

Write-Host ""
Write-Host "OK - artefatos:"
Get-ChildItem dist | ForEach-Object {
    Write-Host ("  {0,-38} {1,8:N1} KB" -f $_.Name, ($_.Length / 1KB))
}
