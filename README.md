# Distributed Service Registry

Registro servizi distribuito in Go con replica gossip tra nodi registry, eseguibile interamente sotto Docker.

## Requisiti pratici

- Go 1.24+
- Docker Desktop attivo per il cluster
- PowerShell 5+ (Windows) oppure Bash (Linux/macOS)

## Comandi rapidi

Il runner `scripts/dev.ps1` raccoglie i comandi più usati senza richiedere GNU Make:

```powershell
.\scripts\dev.ps1 help
.\scripts\dev.ps1 build
.\scripts\dev.ps1 test
.\scripts\dev.ps1 up
.\scripts\dev.ps1 status
.\scripts\dev.ps1 list
.\scripts\dev.ps1 cli register -targets registry-node-1:50051 -name identity-api -id identity-1 -endpoint 203.0.113.10:8080 -version v1.0.0
.\scripts\dev.ps1 down
```

## Scenario completo

Avvio cluster registry:

```powershell
.\scripts\dev.ps1 up
```

Esecuzione completa dello scenario richiesto dalla traccia:

```powershell
.\scripts\dev.ps1 trace
```

Lo scenario `trace` usa `identity-api` e gestisce automaticamente registrazione, gossip, fault injection, recovery e deregistrazione.

I quattro profili disponibili sono `identity-api`, `billing-api`, `payments-api` e `catalog-api`.

Pulizia finale:

```powershell
.\scripts\dev.ps1 down
```

## Cosa copre `dev.ps1 trace`

1. Avvio dei 3 nodi registry
2. Registrazione di un servizio di test
3. Verifica convergenza su tutti i nodi
4. Invio heartbeat
5. Crash di un nodo registry
6. Verifica resilienza dei nodi rimanenti
7. Recovery del nodo crashato
8. Deregistrazione del servizio e verifica stato finale

## Layout

- `cmd/`: entrypoint eseguibili
- `internal/`: dominio, applicazione, storage, gossip e adapter
- `pkg/api/`: codice Go generato dal contratto gRPC
- `proto/`: contratti protobuf
- `deploy/`: Dockerfile e Compose
- `config/registry/`: configurazioni complete dei nodi registry
- `scripts/`: comandi di sviluppo rapidi

## Comandi CLI manuali

Lista servizi da un nodo:

```powershell
.\scripts\dev.ps1 cli list -targets registry-node-1:50051
```

Ricerca interattiva di un servizio:

```powershell
.\scripts\dev.ps1 discovery
```

Oppure ricerca diretta senza prompt:

```powershell
.\scripts\dev.ps1 discovery -name identity-api -id identity-1
```

Registrazione manuale:

```powershell
.\scripts\dev.ps1 cli register -targets registry-node-1:50051,registry-node-2:50051,registry-node-3:50051 -name identity-api -id identity-1 -endpoint 203.0.113.10:8080 -version v1.0.0
```

Heartbeat manuale:

```powershell
.\scripts\dev.ps1 cli heartbeat -targets registry-node-2:50051 -name identity-api -id identity-1
```

Deregistrazione manuale:

```powershell
.\scripts\dev.ps1 cli deregister -targets registry-node-1:50051 -name identity-api -id identity-1
```
