# build.ps1 - compila o app, o instalador e o pacote da release
# Uso:  powershell -ExecutionPolicy Bypass -File build.ps1 [-Version 1.0.1]
param([string]$Version = "1.0.1")
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $root

# saneamento rápido do código
go vet ./...
if ($LASTEXITCODE -ne 0) { throw "go vet falhou" }
$off = gofmt -l .
if ($off) { throw "arquivos fora do padrao gofmt: $($off -join ', ')" }

# recursos Win32 do binário Go (ícone + VersionInfo + manifestação) —
# go-winres gera o .syso, que o `go build` linka automaticamente.
$ver4 = ($Version -split '\.')[0..3] -join '.'
while (($ver4 -split '\.').Count -lt 4) { $ver4 += ".0" }
$winresJson = "winres\winres.json"
$tmpJson = "winres\winres.build.json" # junto dos PNGs (caminhos relativos)
(Get-Content $winresJson -Raw) `
    -replace '"version": "[0-9.]+"', "`"version`": `"$ver4`"" `
    -replace '"file_version": "[0-9.]+"', "`"file_version`": `"$ver4`"" `
    -replace '"product_version": "[0-9.]+"', "`"product_version`": `"$ver4`"" `
    -replace '"FileVersion": "[0-9.]+"', "`"FileVersion`": `"$ver4`"" `
    -replace '"ProductVersion": "[0-9.]+"', "`"ProductVersion`": `"$ver4`"" |
    ForEach-Object { [System.IO.File]::WriteAllText($tmpJson, $_, (New-Object System.Text.UTF8Encoding($false))) }

Write-Host "[1/5] gerando recursos Win32 (versao $ver4) ..."
go run github.com/tc-hib/go-winres@v0.3.3 make --in $tmpJson --arch amd64
if ($LASTEXITCODE -ne 0) { throw "go-winres falhou" }
Remove-Item $tmpJson -Force

Write-Host "[2/5] compilando batterytray.exe ..."
$env:CGO_ENABLED = "1"
go build -ldflags "-s -w -H windowsgui" -o batterytray.exe .
if ($LASTEXITCODE -ne 0) { throw "go build falhou" }

$csc = Join-Path $env:WINDIR "Microsoft.NET\Framework64\v4.0.30319\csc.exe"
if (-not (Test-Path $csc)) { throw "csc nao encontrado: $csc" }

New-Item -ItemType Directory -Force -Path dist | Out-Null

Write-Host "[3/5] compilando Setup.exe (app embutido + manifest asInvoker) ..."
& $csc /nologo /target:winexe /optimize+ /platform:anycpu `
    /out:dist\Setup.exe /win32icon:icon.ico /win32manifest:installer\setup.manifest `
    /resource:batterytray.exe,BatteryTraySetup.app `
    /r:System.dll /r:System.Drawing.dll /r:System.Windows.Forms.dll `
    installer\setup.cs
if ($LASTEXITCODE -ne 0) { throw "csc falhou" }

Write-Host "[4/5] reunindo artefatos em dist\ ..."
Copy-Item batterytray.exe dist\ -Force
Copy-Item README.md dist\LEIA-ME.md -Force

Write-Host "[5/5] gerando zip da release ..."
$zip = "dist\Bateria-Dispositivos-v$Version.zip"
if (Test-Path $zip) { Remove-Item $zip -Force }
Compress-Archive -Path dist\batterytray.exe, dist\Setup.exe, dist\LEIA-ME.md `
    -DestinationPath $zip -Force

Write-Host ""
Write-Host "OK - artefatos:"
Get-ChildItem dist | ForEach-Object {
    Write-Host ("  {0,-38} {1,8:N1} KB" -f $_.Name, ($_.Length / 1KB))
}
Write-Host ""
Write-Host "hashes SHA256:"
Get-ChildItem dist\batterytray.exe, dist\Setup.exe, $zip | ForEach-Object {
    Write-Host ("  {0,-38} {1}" -f $_.Name, (Get-FileHash $_.FullName -Algorithm SHA256).Hash)
}
