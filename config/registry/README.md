# Registry configuration

Questa cartella contiene la configurazione runtime dei nodi registry.

- `example.yaml`: profilo per esecuzione locale o come modello.
- `node-1.yaml` fino a `node-5.yaml`: profili Docker del cluster a cinque nodi.

Ogni file e completo e validato da `internal/config`; non esistono override impliciti o file base da concatenare. Per aggiungere un nodo, duplicare il profilo di un nodo esistente e aggiornare almeno `node.id` e `node.advertise_address`.

Le immagini Docker ricevono questa cartella in `/app/config`. Il percorso predefinito usato da `cmd/registry` e `config/registry/example.yaml`.
