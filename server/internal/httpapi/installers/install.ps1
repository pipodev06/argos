# Instalador del agente argos (Windows). Script ESTATICO: no lleva token ni URL incrustados.
#
# Uso (PowerShell como administrador; el comando completo lo genera el dashboard):
#   -Server <URL>    origen del server (https://, o http://localhost en desarrollo)
#   -Token <TOKEN>   enroll token. Si se omite, o se define ARGOS_ENROLL_TOKEN, no queda en el historial de
#                    comandos; sin ninguno de los dos se pide por consola sin eco.
#   -Ca <archivo>    CA privada (PEM) del server: el agente la copia a su estado. La descarga usa el almacen
#                    de certificados de Windows: importa la CA ahi si el server no usa una CA publica.
#
# Descarga el binario, verifica su SHA-256 contra el manifiesto y ABORTA si no coincide. No se auto-eleva.
# El token llega al agente por entrada estandar (enroll --token-stdin): no se escribe en disco ni en logs.
# Todo el codigo esta dentro de Main, que se invoca en la ULTIMA linea.
param(
    [Parameter(Mandatory = $true)][string]$Server,
    [string]$Token,
    [string]$Ca
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

function Fail([string]$Message) {
    throw "argos install: $Message"
}

function Invoke-Agent([string]$Exe, [string[]]$AgentArgs) {
    & $Exe @AgentArgs
    if ($LASTEXITCODE -ne 0) { Fail "argos-agent $($AgentArgs[0]) fallo (codigo $LASTEXITCODE)" }
}

function Main([string]$Server, [string]$Token, [string]$Ca) {
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    if (-not ([Security.Principal.WindowsPrincipal]$identity).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
        Fail 'ejecutalo en una consola de PowerShell como administrador'
    }

    $Server = $Server.TrimEnd('/')
    if ($Server -notmatch '^https://' -and $Server -notmatch '^http://(localhost|127\.0\.0\.1)(:\d+)?$') {
        Fail 'el server debe ser https:// (o http://localhost en desarrollo)'
    }
    if ($Ca -and -not (Test-Path -LiteralPath $Ca -PathType Leaf)) { Fail "no se puede leer la CA: $Ca" }

    if (-not $Token) { $Token = $env:ARGOS_ENROLL_TOKEN }
    if (-not $Token) {
        $secure = Read-Host -AsSecureString 'Enroll token'
        $Token = [Runtime.InteropServices.Marshal]::PtrToStringBSTR([Runtime.InteropServices.Marshal]::SecureStringToBSTR($secure))
    }
    if (-not $Token) { Fail 'el enroll token esta vacio' }

    $arch = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
    switch ($arch) {
        'AMD64' { $goarch = 'amd64' }
        'ARM64' { $goarch = 'arm64' }
        default { Fail "arquitectura no soportada: $arch" }
    }
    $name = "argos-agent_windows_$goarch.exe"

    $tmp = Join-Path ([IO.Path]::GetTempPath()) ("argos-install-" + [Guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $tmp | Out-Null
    try {
        $manifest = Invoke-RestMethod -UseBasicParsing -Uri "$Server/dl/latest/manifest.json"
        $version = [string]$manifest.version
        $entry = @($manifest.files | Where-Object { $_.name -eq $name })[0]
        if ($version -notmatch '^[0-9A-Za-z][0-9A-Za-z._+-]{0,63}$' -or -not $entry -or [string]$entry.sha256 -notmatch '^[0-9a-f]{64}$') {
            Fail "el manifiesto no tiene $name"
        }

        $download = Join-Path $tmp 'agent.exe'
        Invoke-WebRequest -UseBasicParsing -Uri "$Server/dl/$version/$name" -OutFile $download
        $got = (Get-FileHash -Algorithm SHA256 -LiteralPath $download).Hash.ToLowerInvariant()
        if ($got -ne [string]$entry.sha256) { Fail 'SHA-256 del binario no coincide con el manifiesto: abortando' }

        $dir = Join-Path $env:ProgramFiles 'argos-agent'
        New-Item -ItemType Directory -Force -Path $dir | Out-Null
        $exe = Join-Path $dir 'argos-agent.exe'
        Copy-Item -LiteralPath $download -Destination $exe -Force

        if ($Ca) { Invoke-Agent $exe @('install', '--ca-file', $Ca) } else { Invoke-Agent $exe @('install') }
        $Token | & $exe enroll --server $Server --token-stdin
        if ($LASTEXITCODE -ne 0) { Fail "argos-agent enroll fallo (codigo $LASTEXITCODE)" }
        $Token = $null
        Invoke-Agent $exe @('start')
        Write-Host "argos install: agente $version instalado y en marcha"
    }
    finally {
        Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
    }
}

Main -Server $Server -Token $Token -Ca $Ca
