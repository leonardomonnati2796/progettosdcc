# Distributed Service Registry

Registro servizi distribuito in Go con replica gossip tra nodi registry, eseguibile interamente sotto Docker.

## Requisiti pratici

- Docker Desktop attivo
- `make` disponibile nel terminale

## Comandi principali

Avvio cluster registry:

```powershell
make trace-up
```

Esecuzione completa dello scenario richiesto dalla traccia:

```powershell
make trace-cover
```

Durante `trace-cover` puoi scegliere in modo interattivo quale servizio registrare prima della fase 1: `users-api` oppure `orders-api`.
Se vuoi evitare il prompt, puoi selezionarlo in modo esplicito con `TRACE_SERVICE_PROFILE=users` oppure `TRACE_SERVICE_PROFILE=orders`.

Pulizia finale:

```powershell
make compose-down
```

## Cosa copre `make trace-cover`

1. Avvio dei 3 nodi registry
2. Registrazione di un servizio di test
3. Verifica convergenza su tutti i nodi
4. Invio heartbeat
5. Crash di un nodo registry
6. Verifica resilienza dei nodi rimanenti
7. Recovery del nodo crashato
8. Deregistrazione del servizio e verifica stato finale

## Comandi manuali utili

Lista servizi da un nodo:

```powershell
make docker-service-cli ARGS="list -targets registry-node-1:50051"
```

Registrazione manuale:

```powershell
make docker-service-cli ARGS="register -targets registry-node-1:50051,registry-node-2:50051,registry-node-3:50051 -name users-api -id users-1 -endpoint 203.0.113.10:8080 -version v1.0.0"
```

Heartbeat manuale:

```powershell
make docker-service-cli ARGS="heartbeat -targets registry-node-2:50051 -name users-api -id users-1"
```

Deregistrazione manuale:

```powershell
make docker-service-cli ARGS="deregister -targets registry-node-1:50051 -name users-api -id users-1"
```
