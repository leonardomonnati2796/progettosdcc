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
$Targets = "registry-node-1:10001,registry-node-2:10001,registry-node-3:10001,registry-node-4:10001,registry-node-5:10001"
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
    if (-not (Test-Path $ServiceStateFile)) {
        throw "Nessun servizio selezionato. Esegui prima '.\scripts\dev.ps1 select-service'."
    }
    $state = @{}
    Get-Content $ServiceStateFile | ForEach-Object {
        $parts = $_ -split "=", 2
        if ($parts.Count -eq 2) { $state[$parts[0]] = $parts[1] }
    }
    if ([string]::IsNullOrWhiteSpace($state["TRACE_SERVICE_NAME"]) -or
        [string]::IsNullOrWhiteSpace($state["TRACE_SERVICE_ENDPOINT"])) {
        throw "Nessun servizio selezionato. Esegui prima '.\scripts\dev.ps1 select-service'."
    }
    return $state
}

function Get-RandomRegistryNode {
    return Get-RegistryNodes | Get-Random
}

function Get-RegistryNodes {
    return @("registry-node-1", "registry-node-2", "registry-node-3", "registry-node-4", "registry-node-5")
}

function Get-CrashNodes {
    if (Test-Path $CrashStateFile) {
        return @(Get-Content $CrashStateFile | ForEach-Object { $_.Trim() } | Where-Object { $_ })
    }
    return @()
}

function Show-ClusterServices {
    foreach ($node in Get-RegistryNodes) {
        Write-Host "--- $node`:10001 ---"
        Invoke-Cli @("list", "-targets", "$node`:10001")
    }
}

try {
switch ($Command.ToLowerInvariant()) {
    "help" {
        Write-Host @"
Usage: .\scripts\dev.ps1 <command> [args]

Go:      build | build-cli | test | test-integration | proto | tidy | lint
Cluster: trace-up | select-service | register | discovery | crash | recover | verify-resilience | deregister | down
         up | status | logs | list
CLI:     cli <register|deregister|list|get> [flags]

Crash:   crash arresta un nodo casuale; crash -count N arresta N nodi casuali (massimo N-2)

Scripts list:
  .\scripts\dev.ps1 trace-up
  .\scripts\dev.ps1 select-service
  .\scripts\dev.ps1 register
  .\scripts\dev.ps1 discovery
  .\scripts\dev.ps1 deregister
  .\scripts\dev.ps1 down
    .\scripts\dev.ps1 crash
    .\scripts\dev.ps1 recover
"@
    }
    "select-service" {
        $profile = Get-Option $Arguments "-profile"
        $endpointOverride = Get-Option $Arguments "-endpoint"
        $profileWasRandom = [string]::IsNullOrWhiteSpace($profile) -or $profile -eq "users" -or $profile -eq "random"
        if (-not $profile -or $profile -eq "users" -or $profile -eq "random") {
            $profile = @("identity", "billing", "payments", "catalog") | Get-Random
        }
        switch ($profile.ToLowerInvariant()) {
            "2" { $name = "billing-api"; $endpoint = "203.0.113.20:8080" }
            "billing" { $name = "billing-api"; $endpoint = "203.0.113.20:8080" }
            "billing-api" { $name = "billing-api"; $endpoint = "203.0.113.20:8080" }
            "3" { $name = "payments-api"; $endpoint = "203.0.113.30:8080" }
            "payments" { $name = "payments-api"; $endpoint = "203.0.113.30:8080" }
            "payments-api" { $name = "payments-api"; $endpoint = "203.0.113.30:8080" }
            "4" { $name = "catalog-api"; $endpoint = "203.0.113.40:8080" }
            "catalog" { $name = "catalog-api"; $endpoint = "203.0.113.40:8080" }
            "catalog-api" { $name = "catalog-api"; $endpoint = "203.0.113.40:8080" }
            "identity-api" { $name = "identity-api"; $endpoint = "203.0.113.10:8080" }
            default { $name = "identity-api"; $endpoint = "203.0.113.10:8080" }
        }
        if (-not [string]::IsNullOrWhiteSpace($endpointOverride)) {
            $endpoint = $endpointOverride.Trim()
        }
        @("TRACE_SERVICE_NAME=$name", "TRACE_SERVICE_ENDPOINT=$endpoint") | Set-Content $ServiceStateFile
        if ($profileWasRandom) {
            Write-Host "Servizio selezionato casualmente: $name"
        }
        else {
            Write-Host "Servizio selezionato: $name"
        }
    }
    "register" {
        $state = Read-ServiceState
        $serviceTargets = Get-Option $Arguments "-targets" $Targets
        Invoke-Cli @("register", "-targets", $serviceTargets, "-name", $state["TRACE_SERVICE_NAME"], "-endpoint", $state["TRACE_SERVICE_ENDPOINT"])
        Start-Sleep -Seconds 3
        Show-ClusterServices
    }
    "crash" {
        $count = [int](Get-Option $Arguments "-count" "1")
        $registryNodes = @(Get-RegistryNodes)
        $availableNodes = @($registryNodes | Where-Object { $_ -notin @(Get-CrashNodes) })
        $maxCrashCount = $registryNodes.Count - 2
        if ($count -lt 1 -or $count -gt $maxCrashCount -or $count -gt $availableNodes.Count) {
            throw "-count deve essere compreso tra 1 e $maxCrashCount, lasciando almeno due nodi attivi."
        }

        $nodes = @($availableNodes | Sort-Object { Get-Random } | Select-Object -First $count)
        $nodes | Set-Content $CrashStateFile
        Invoke-Compose (@("stop") + $nodes)
        Write-Host "Fault injection: arrestati $($nodes -join ', ')."
    }
    "recover" {
        $nodes = @(Get-CrashNodes)
        if ($nodes.Count -eq 0) { throw "Nessun nodo in crash da recuperare." }
        Invoke-Compose (@("start") + $nodes)
        Remove-Item $CrashStateFile -Force -ErrorAction SilentlyContinue
        Write-Host "Recovery: riavviati $($nodes -join ', ')."
    }
    "verify-resilience" {
        $crashedNodes = @(Get-CrashNodes)
        $activeTargets = Get-RegistryNodes | Where-Object { $_ -notin $crashedNodes } | ForEach-Object { "$_`:10001" }
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
        $serviceName = Get-Option $Arguments "-name"
        if ([string]::IsNullOrWhiteSpace($serviceName)) {
            $serviceName = Read-Host "Inserisci il nome del servizio da deregistrare"
        }
        if ([string]::IsNullOrWhiteSpace($serviceName)) {
            throw "Il nome del servizio è obbligatorio."
        }
        Invoke-Cli @("deregister", "-targets", (Get-Option $Arguments "-targets" $Targets), "-name", $serviceName.Trim())
        Start-Sleep -Seconds 3
        Show-ClusterServices
    }
    "up" { Remove-Item $ServiceStateFile, $CrashStateFile -Force -ErrorAction SilentlyContinue; Invoke-Compose (@("build") + (Get-RegistryNodes) + @("service-cli")); Invoke-Compose (@("up", "-d") + (Get-RegistryNodes)) }
    "build-cli" { Invoke-Compose @("build", "service-cli") }
    "down" { Invoke-Compose @("down", "--remove-orphans"); Remove-Item $StateFile, $ServiceStateFile, $CrashStateFile -Force -ErrorAction SilentlyContinue }
    "logs" { Invoke-Compose @("logs", "-f") }
    "status" { Invoke-Compose @("ps") }
    "list" { Show-ClusterServices }
    "cli" {
        if (-not $Arguments) { throw "Specifica un comando CLI." }
        Invoke-Cli $Arguments
    }
    default { throw "Comando sconosciuto '$Command'. Usa '.\scripts\dev.ps1 help'." }
}
}
catch {
    Write-Host $_.Exception.Message -ForegroundColor Red
    exit 1
}
