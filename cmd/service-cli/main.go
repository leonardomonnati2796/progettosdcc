package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/leonardomonnati2796/distributed-service-registry/internal/client"
	apiv1 "github.com/leonardomonnati2796/distributed-service-registry/pkg/api"
)

type outputServiceRecord struct {
	ServiceName       string `json:"service_name,omitempty"`
	ServiceId         string `json:"service_id,omitempty"`
	Endpoint          string `json:"endpoint,omitempty"`
	Version           string `json:"version,omitempty"`
	LastHeartbeatUnix int64  `json:"last_heartbeat_unix,omitempty"`
	OwnerNodeId       string `json:"owner_node_id,omitempty"`
	LogicalVersion    uint64 `json:"logical_version,omitempty"`
}

func main() {
	// Avvia il programma.
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]
	switch command {
	case "register":
		runRegister(os.Args[2:])
	case "heartbeat":
		runHeartbeat(os.Args[2:])
	case "deregister":
		runDeregister(os.Args[2:])
	case "list":
		runList(os.Args[2:])
	case "get":
		runGet(os.Args[2:])
	case "help", "-h", "--help":
		printUsage()
	default:
		log.Fatalf("unknown command %q", command)
	}
}

func printUsage() {
	// Stampa il contenuto richiesto.
	fmt.Fprintf(os.Stderr, "service-cli <command> [flags]\n")
	fmt.Fprintf(os.Stderr, "commands: register, heartbeat, deregister, list, get\n")
	fmt.Fprintf(os.Stderr, "common flag: -targets localhost:50051,localhost:50052\n")
}

func newClient(targets string) *client.RegistryClient {
	// Crea un nuovo client.
	endpoints := make([]string, 0)
	for _, target := range strings.Split(targets, ",") {
		trimmed := strings.TrimSpace(target)
		if trimmed == "" {
			continue
		}
		endpoints = append(endpoints, trimmed)
	}
	return client.NewRegistryClient(endpoints, 3*time.Second, 5*time.Second)
}

func runRegister(args []string) {
	// Esegue il comando richiesto.
	fs := flag.NewFlagSet("register", flag.ExitOnError)
	targets := fs.String("targets", "localhost:50051", "comma-separated registry endpoints")
	serviceName := fs.String("name", "", "service name")
	serviceID := fs.String("id", "", "service id")
	endpoint := fs.String("endpoint", "", "service endpoint")
	version := fs.String("version", "", "service version")
	health := fs.String("health", "HEALTH_STATUS_SERVING", "health status")
	traceRegister := fs.Bool("trace-register", false, "print register workflow trace")
	fs.Parse(args)

	requireFlag(*serviceName, "name")
	requireFlag(*serviceID, "id")
	requireFlag(*endpoint, "endpoint")
	requireFlag(*version, "version")

	rc := newClient(*targets)
	if *traceRegister {
		rc.SetRegisterTrace(true, os.Stderr)
	}
	resp, err := rc.RegisterWithResponse(&apiv1.ServiceRecord{
		ServiceName:  *serviceName,
		ServiceId:    *serviceID,
		Endpoint:     *endpoint,
		Version:      *version,
		HealthStatus: parseHealthStatus(*health),
	})
	if err != nil {
		log.Fatal(err)
	}
	if strings.Contains(strings.ToLower(resp.GetMessage()), "already registered") {
		fmt.Println("servizio già presente: " + resp.GetMessage())
		return
	}
	fmt.Println("servizio registrato")
}

func runHeartbeat(args []string) {
	// Esegue il comando richiesto.
	fs := flag.NewFlagSet("heartbeat", flag.ExitOnError)
	targets := fs.String("targets", "localhost:50051", "comma-separated registry endpoints")
	serviceName := fs.String("name", "", "service name")
	serviceID := fs.String("id", "", "service id")
	health := fs.String("health", "HEALTH_STATUS_SERVING", "health status")
	fs.Parse(args)

	requireFlag(*serviceName, "name")
	requireFlag(*serviceID, "id")

	rc := newClient(*targets)
	if err := rc.Heartbeat(*serviceName, *serviceID, parseHealthStatus(*health)); err != nil {
		log.Fatal(err)
	}
	fmt.Println("heartbeat ok")
}

func runDeregister(args []string) {
	// Esegue il comando richiesto.
	fs := flag.NewFlagSet("deregister", flag.ExitOnError)
	targets := fs.String("targets", "localhost:50051", "comma-separated registry endpoints")
	serviceName := fs.String("name", "", "service name")
	serviceID := fs.String("id", "", "service id")
	fs.Parse(args)

	requireFlag(*serviceName, "name")
	requireFlag(*serviceID, "id")

	rc := newClient(*targets)
	if err := rc.Deregister(*serviceName, *serviceID); err != nil {
		if isDeregisterNotFoundError(err) {
			fmt.Printf("servizio non presente: %s/%s; niente da deregistrare\n", *serviceName, *serviceID)
			return
		}
		log.Fatal(err)
	}
	fmt.Println("deregister ok")
}

func runList(args []string) {
	// Esegue il comando richiesto.
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	targets := fs.String("targets", "localhost:50051", "comma-separated registry endpoints")
	fs.Parse(args)

	nodeTarget := primaryTarget(*targets)
	rc := newClient(*targets)
	records, err := rc.List()
	if err != nil {
		if isUnavailableTargetError(err) {
			fmt.Fprintf(os.Stderr, "nodo disattivato: %s\n", nodeTarget)
			return
		}
		log.Fatal(err)
	}
	printJSON(map[string]any{
		"nodo":    nodeTarget,
		"servizi": toOutputServiceRecords(records),
	})
}

func runGet(args []string) {
	// Esegue il comando richiesto.
	fs := flag.NewFlagSet("get", flag.ExitOnError)
	targets := fs.String("targets", "localhost:50051", "comma-separated registry endpoints")
	serviceName := fs.String("name", "", "service name")
	serviceID := fs.String("id", "", "service id")
	fs.Parse(args)

	requireFlag(*serviceName, "name")

	rc := newClient(*targets)
	records, err := rc.Get(*serviceName, *serviceID)
	if err != nil {
		log.Fatal(err)
	}
	printJSON(toOutputServiceRecords(records))
}

func toOutputServiceRecords(records []*apiv1.ServiceRecord) []outputServiceRecord {
	// Converte i dati nel formato richiesto.
	out := make([]outputServiceRecord, 0, len(records))
	for _, record := range records {
		if record == nil {
			continue
		}
		out = append(out, outputServiceRecord{
			ServiceName:       record.GetServiceName(),
			ServiceId:         record.GetServiceId(),
			Endpoint:          record.GetEndpoint(),
			Version:           record.GetVersion(),
			LastHeartbeatUnix: record.GetLastHeartbeatUnix(),
			OwnerNodeId:       record.GetOwnerNodeId(),
			LogicalVersion:    record.GetLogicalVersion(),
		})
	}
	return out
}

func requireFlag(value string, name string) {
	// Esegue la logica di require flag.
	if strings.TrimSpace(value) == "" {
		log.Fatalf("-%s is required", name)
	}
}

func parseHealthStatus(value string) apiv1.HealthStatus {
	// Esegue la logica di parse health status.
	switch strings.TrimSpace(value) {
	case "HEALTH_STATUS_NOT_SERVING":
		return apiv1.HealthStatus_HEALTH_STATUS_NOT_SERVING
	case "HEALTH_STATUS_DEGRADED":
		return apiv1.HealthStatus_HEALTH_STATUS_DEGRADED
	case "HEALTH_STATUS_UNSPECIFIED":
		return apiv1.HealthStatus_HEALTH_STATUS_UNSPECIFIED
	default:
		return apiv1.HealthStatus_HEALTH_STATUS_SERVING
	}
}

func printJSON(value any) {
	// Stampa il contenuto richiesto.
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(data))
}

func primaryTarget(targets string) string {
	// Esegue la logica di primary target.
	for _, target := range strings.Split(targets, ",") {
		trimmed := strings.TrimSpace(target)
		if trimmed != "" {
			return trimmed
		}
	}
	return strings.TrimSpace(targets)
}

func isUnavailableTargetError(err error) bool {
	// Verifica la condizione richiesta.
	if err == nil {
		return false
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	message := strings.ToLower(err.Error())
	return strings.Contains(message, "context deadline exceeded") ||
		strings.Contains(message, "connection refused") ||
		strings.Contains(message, "rpc error: code = unavailable")
}

func isDeregisterNotFoundError(err error) bool {
	// Verifica la condizione richiesta.
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "deregisterservice rejected: service not found") ||
		strings.Contains(message, "service not found")
}
