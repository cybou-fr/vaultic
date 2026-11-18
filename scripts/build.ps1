$ErrorActionPreference = 'Stop'
Set-Location (Join-Path $PSScriptRoot '..')
go mod tidy
New-Item -ItemType Directory -Force dist | Out-Null
go build -trimpath -ldflags='-s -w -H windowsgui' -o dist/vaultic.exe ./cmd/vaultic
Copy-Item portable.flag dist/portable.flag -Force
Copy-Item LICENSE, NOTICE, THIRD_PARTY_NOTICES.md dist/ -Force
Write-Host 'Готово: dist/vaultic.exe'
Write-Host 'Положите restic.exe рядом с приложением или добавьте restic в PATH.'
