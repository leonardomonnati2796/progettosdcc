# Distributed Service Registry

Registro servizi distribuito in Go con replica gossip tra nodi registry, eseguibile interamente sotto Docker.

## Requisiti pratici

- Go 1.24+
- Docker Desktop attivo per il cluster
- PowerShell 5+ (Windows) oppure Bash (Linux/macOS)

## Installazione su Amazon EC2

Su un'istanza EC2 con Amazon Linux, installare Docker, Go e gli strumenti necessari:

```bash
sudo dnf update -y
sudo dnf install -y docker golang curl
sudo systemctl enable --now docker
sudo usermod -aG docker ec2-user
newgrp docker
```

Su Amazon Linux 2, se `dnf` non e disponibile, usare:

```bash
sudo yum update -y
sudo amazon-linux-extras install docker
sudo yum install -y golang curl
sudo systemctl enable --now docker
```

Verificare Docker, Compose e Go:

```bash
docker ps
docker compose version
go version
```

Se Docker Compose non e disponibile, installare il plugin per l'utente corrente:

```bash
mkdir -p ~/.docker/cli-plugins
curl -fL https://github.com/docker/compose/releases/latest/download/docker-compose-linux-x86_64 \
	-o ~/.docker/cli-plugins/docker-compose
chmod +x ~/.docker/cli-plugins/docker-compose
docker compose version
```

Il progetto usa Docker Buildx per la build delle immagini. Installare una versione aggiornata:

```bash
BUILDX_VERSION=$(curl -fsSL https://api.github.com/repos/docker/buildx/releases/latest \
	| grep '"tag_name"' \
	| sed -E 's/.*"([^"]+)".*/\1/')

sudo mkdir -p /usr/local/lib/docker/cli-plugins
sudo curl -fL \
	"https://github.com/docker/buildx/releases/download/${BUILDX_VERSION}/buildx-${BUILDX_VERSION}.linux-amd64" \
	-o /usr/local/lib/docker/cli-plugins/docker-buildx
sudo chmod +x /usr/local/lib/docker/cli-plugins/docker-buildx
docker buildx version
```

Per eseguire lo script PowerShell su Amazon Linux, installare ICU e PowerShell:

```bash
sudo dnf install -y libicu

cd /tmp
VERSION=7.5.3
curl -fLO https://github.com/PowerShell/PowerShell/releases/download/v${VERSION}/powershell-${VERSION}-linux-x64.tar.gz
sudo mkdir -p /opt/microsoft/powershell/7
sudo tar -xzf powershell-${VERSION}-linux-x64.tar.gz -C /opt/microsoft/powershell/7
sudo chmod +x /opt/microsoft/powershell/7/pwsh
sudo ln -sf /opt/microsoft/powershell/7/pwsh /usr/local/bin/pwsh
pwsh --version
```

Clonare o copiare il progetto nella directory prevista e verificare lo script:

```bash
cd /home/ec2-user/progettosdcc
ls -l scripts/dev.ps1
pwsh -File ./scripts/dev.ps1 help
```

Avviare quindi il cluster a cinque nodi:

```bash
pwsh -File ./scripts/dev.ps1 trace-up
pwsh -File ./scripts/dev.ps1 select-service -profile random
pwsh -File ./scripts/dev.ps1 register
pwsh -File ./scripts/dev.ps1 list
```

Se Docker segnala un errore di permessi su `/var/run/docker.sock`, chiudere la sessione SSH e accedere nuovamente dopo l'aggiunta dell'utente al gruppo `docker`:

```bash
exit
```

Per permettere connessioni esterne, configurare nel Security Group AWS solo le porte necessarie (`22` per SSH e, se richiesto, `50051`--`50055` per gRPC). Per l'uso ordinario e preferibile non esporre pubblicamente le porte gRPC.

## Comandi operativi

Eseguire i comandi dalla directory principale del progetto. Su EC2 Linux usare
`pwsh -File`; su Windows è possibile usare direttamente
`./scripts/dev.ps1`.

```bash
cd /home/ec2-user/progettosdcc
pwsh -File ./scripts/dev.ps1 help
pwsh -File ./scripts/dev.ps1 trace-up
```

Selezionare e registrare un servizio:

```bash
pwsh -File ./scripts/dev.ps1 select-service -profile random
pwsh -File ./scripts/dev.ps1 register
pwsh -File ./scripts/dev.ps1 list
```

La registrazione viene propagata automaticamente ai cinque nodi tramite gossip.
Per aggiornare il Lamport clock dello stesso servizio, selezionare un endpoint
diverso e registrare nuovamente:

```bash
pwsh -File ./scripts/dev.ps1 select-service -profile billing -endpoint 203.0.113.21:8080
pwsh -File ./scripts/dev.ps1 register
```

Eseguire una discovery:

```bash
pwsh -File ./scripts/dev.ps1 discovery -name billing-api
```

Simulare un crash e verificare la resilienza:

```bash
pwsh -File ./scripts/dev.ps1 crash
pwsh -File ./scripts/dev.ps1 verify-resilience
pwsh -File ./scripts/dev.ps1 recover
```

Il comando `crash` senza `-count` arresta un nodo casuale. Con `-count N` è
possibile arrestare più nodi casuali, fino a `N-2`, lasciando almeno due nodi
attivi:

```bash
pwsh -File ./scripts/dev.ps1 crash -count 2
pwsh -File ./scripts/dev.ps1 crash -count 3
```

Deregistrare un servizio e visualizzare il Lamport clock del deletion marker:

```bash
pwsh -File ./scripts/dev.ps1 deregister
```

Il comando chiede interattivamente il nome del servizio.

Infine, arrestare e rimuovere l'ambiente Docker:

```bash
pwsh -File ./scripts/dev.ps1 down
```

## Layout

- `cmd/`: entrypoint eseguibili
- `internal/`: dominio, applicazione, storage, gossip e adapter
- `pkg/api/`: codice Go generato dal contratto gRPC
- `proto/`: contratti protobuf
- `deploy/`: Dockerfile e Compose
- `config/registry/`: configurazioni dei cinque nodi registry
- `scripts/`: comandi operativi
