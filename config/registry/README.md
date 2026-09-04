# Registry configuration

Questa cartella contiene la configurazione runtime dei nodi registry.

- `example.yaml`: profilo per esecuzione locale o come modello.
- `node-1.yaml`, `node-2.yaml`, `node-3.yaml`: profili Docker del cluster a tre nodi.

Ogni file e completo e validato da `internal/config`; non esistono override impliciti o file base da concatenare. Per aggiungere un nodo, duplicare il profilo di un nodo esistente e aggiornare almeno `node.id`, `node.advertise_address` e l'eventuale `storage_dir`.

Le immagini Docker ricevono questa cartella in `/app/config`. Il percorso predefinito usato da `cmd/registry` e `config/registry/example.yaml`.
