[CmdletBinding()]
param(
    [Parameter(Position = 0)]
    [string]$Command = "help",
    [Parameter(ValueFromRemainingArguments = $true)]
    [string[]]$Arguments
)

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot
$ComposeFile = Join-Path $Root "deploy/docker-compose.yml"
$Targets = "registry-node-1:50051,registry-node-2:50051,registry-node-3:50051"
$StateFile = Join-Path $Root ".trace-up.state"
$ServiceStateFile = Join-Path $Root ".trace-service.state"
$CrashStateFile = Join-Path $Root ".trace-crash.state"

function Invoke-Compose {
    param([Parameter(Mandatory = $true)][string[]]$ComposeArguments)
    & docker compose -f $ComposeFile --project-directory $Root @ComposeArguments
    if ($LASTEXITCODE -ne 0) { throw "docker compose ha restituito codice $LASTEXITCODE" }
}

function Invoke-Cli {
    param([Parameter(Mandatory = $true)][string[]]$CliArguments)
    Invoke-Compose (@("run", "--rm", "service-cli") + $CliArguments)
}

function Get-Option {
    param([string[]]$Values, [string]$Name, [string]$Default = "")
    if ($null -eq $Values -or $Values.Count -eq 0) { return $Default }
    $index = [Array]::IndexOf($Values, $Name)
    if ($index -ge 0 -and $index + 1 -lt $Values.Count) { return $Values[$index + 1] }
    return $Default
}

function Read-ServiceState {
    if (-not (Test-Path $ServiceStateFile)) { & $PSCommandPath select-service }
    $state = @{}
    Get-Content $ServiceStateFile | ForEach-Object {
        $parts = $_ -split "=", 2
        if ($parts.Count -eq 2) { $state[$parts[0]] = $parts[1] }
    }
    return $state
}

function Enable-Gossip {
    foreach ($node in @("registry-node-1", "registry-node-2", "registry-node-3")) {
        Invoke-Compose @("exec", "-T", $node, "sh", "-c", "touch /app/data/.gossip-enabled")
    }
}

function Get-RandomRegistryNode {
    return @("registry-node-1", "registry-node-2", "registry-node-3") | Get-Random
}

function Get-CrashNode {
    if (Test-Path $CrashStateFile) {
        return (Get-Content $CrashStateFile | Select-Object -First 1).Trim()
    }
    return Get-RandomRegistryNode
}

switch ($Command.ToLowerInvariant()) {
    "help" {
        Write-Host @"
Usage: .\scripts\dev.ps1 <command> [args]

Go:      build | build-cli | test | test-integration | proto | tidy | lint
Cluster: trace-up | select-service | register | discovery | crash | recover | verify-resilience | deregister | down
         up | status | logs | list
CLI:     cli <register|deregister|list|get> [flags]

Make equivalents:
  make trace-up       -> .\scripts\dev.ps1 trace-up
  make trace-select-service -> .\scripts\dev.ps1 select-service
  make trace-register -> .\scripts\dev.ps1 register
  make trace-discovery -> .\scripts\dev.ps1 discovery
  make trace-deregister -> .\scripts\dev.ps1 deregister
  make compose-down   -> .\scripts\dev.ps1 down
    fault injection      -> .\scripts\dev.ps1 crash
    fault recovery       -> .\scripts\dev.ps1 recover
"@
    }
    "build" { go build ./... }
    "test" { go test ./... }
    "test-integration" { go test -tags=integration ./internal/integration }
    "proto" {
        $protoc = Get-Command protoc -ErrorAction SilentlyContinue
        if (-not $protoc) { throw "protoc non trovato nel PATH." }
        $goBin = go env GOPATH
        $env:Path = "$(Join-Path $goBin 'bin');$env:Path"
        & $protoc.Source -I=proto --go_out=pkg/api --go_opt=paths=source_relative --go-grpc_out=pkg/api --go-grpc_opt=paths=source_relative (Get-ChildItem proto -Filter *.proto | Select-Object -ExpandProperty FullName)
    }
    "tidy" { go mod tidy }
    "lint" {
        if (-not (Get-Command golangci-lint -ErrorAction SilentlyContinue)) { throw "golangci-lint non trovato nel PATH." }
        golangci-lint run ./...
    }
    "trace-up" {
        Invoke-Compose @("down", "--remove-orphans")
        Invoke-Compose @("up", "-d", "--build", "registry-node-1", "registry-node-2", "registry-node-3")
        Invoke-Compose @("build", "service-cli")
        New-Item $StateFile -ItemType File -Force | Out-Null
        Write-Host "Cluster avviato. Ora esegui select-service e register."
    }
    "select-service" {
        $profile = Get-Option $Arguments "-profile"
        if (-not $profile -or $profile -eq "users" -or $profile -eq "random") {
            $profile = @("identity", "billing", "payments", "catalog") | Get-Random
        }
        switch ($profile.ToLowerInvariant()) {
            "2" { $name = "billing-api"; $endpoint = "203.0.113.20:8080" }
            "billing" { $name = "billing-api"; $endpoint = "203.0.113.20:8080" }
            "3" { $name = "payments-api"; $endpoint = "203.0.113.30:8080" }
            "payments" { $name = "payments-api"; $endpoint = "203.0.113.30:8080" }
            "4" { $name = "catalog-api"; $endpoint = "203.0.113.40:8080" }
            "catalog" { $name = "catalog-api"; $endpoint = "203.0.113.40:8080" }
            default { $name = "identity-api"; $endpoint = "203.0.113.10:8080" }
        }
        @("TRACE_SERVICE_NAME=$name", "TRACE_SERVICE_ENDPOINT=$endpoint") | Set-Content $ServiceStateFile
		Write-Host "Servizio selezionato casualmente: $name"
    }
    "register" {
        $state = Read-ServiceState
        $serviceTargets = Get-Option $Arguments "-targets" $Targets
        Invoke-Cli @("register", "-targets", $serviceTargets, "-name", $state["TRACE_SERVICE_NAME"], "-endpoint", $state["TRACE_SERVICE_ENDPOINT"], "-trace-register", "true")
        Enable-Gossip
        Start-Sleep -Seconds 3
        Invoke-Cli @("list", "-targets", $Targets)
    }
    "crash" {
        $node = Get-RandomRegistryNode
        $node | Set-Content $CrashStateFile
        Invoke-Compose @("stop", $node)
        Write-Host "Fault injection: arrestato casualmente $node."
    }
    "recover" {
        $node = Get-CrashNode
        Invoke-Compose @("start", $node)
        Remove-Item $CrashStateFile -Force -ErrorAction SilentlyContinue
        Write-Host "Recovery: riavviato $node."
    }
    "verify-resilience" {
        $node = Get-CrashNode
        $activeTargets = @("registry-node-1:50051", "registry-node-2:50051", "registry-node-3:50051") | Where-Object { $_ -notlike "$node`:*" }
        Invoke-Cli @("list", "-targets", ($activeTargets -join ","))
    }
    "discovery" {
        $name = Get-Option $Arguments "-name"
        $nameWasProvided = -not [string]::IsNullOrWhiteSpace($name)
        if (-not $nameWasProvided) { $name = Read-Host "Inserisci il nome del servizio da cercare" }
        if (-not $name.Trim()) { throw "Il nome del servizio è obbligatorio." }
        Write-Host "Ricerca servizio: $name"
        Invoke-Cli @("get", "-targets", (Get-Option $Arguments "-targets" $Targets), "-name", $name)
    }
    "deregister" {
        $state = Read-ServiceState
        Invoke-Cli @("deregister", "-targets", (Get-Option $Arguments "-targets" $Targets), "-name", $state["TRACE_SERVICE_NAME"])
        Start-Sleep -Seconds 3
        Invoke-Cli @("list", "-targets", $Targets)
    }
    "up" { Invoke-Compose @("build", "registry-node-1", "registry-node-2", "registry-node-3", "service-cli"); Invoke-Compose @("up", "-d", "registry-node-1", "registry-node-2", "registry-node-3") }
    "build-cli" { Invoke-Compose @("build", "service-cli") }
    "down" { Invoke-Compose @("down", "--remove-orphans"); Remove-Item $StateFile, $ServiceStateFile, $CrashStateFile -Force -ErrorAction SilentlyContinue }
    "logs" { Invoke-Compose @("logs", "-f") }
    "status" { Invoke-Compose @("ps") }
    "list" { Invoke-Cli @("list", "-targets", $Targets) }
    "trace" {
        & $PSCommandPath trace-up
        & $PSCommandPath select-service -profile random
        & $PSCommandPath register
        & $PSCommandPath discovery
        & $PSCommandPath crash
        & $PSCommandPath verify-resilience
        & $PSCommandPath recover
        & $PSCommandPath discovery
        & $PSCommandPath deregister
        & $PSCommandPath down
    }
    "cli" {
        if (-not $Arguments) { throw "Specifica un comando CLI." }
        Invoke-Cli $Arguments
    }
    default { throw "Comando sconosciuto '$Command'. Usa '.\scripts\dev.ps1 help'." }
}
