package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/leonardomonnati2796/distributed-service-registry/internal/client"
	apiv1 "github.com/leonardomonnati2796/distributed-service-registry/pkg/api"
)

func main() {
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
	fmt.Fprintf(os.Stderr, "service-cli <command> [flags]\n")
	fmt.Fprintf(os.Stderr, "commands: register, heartbeat, deregister, list, get\n")
	fmt.Fprintf(os.Stderr, "common flag: -targets localhost:50051,localhost:50052\n")
}

func newClient(targets string) *client.RegistryClient {
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
	fs := flag.NewFlagSet("register", flag.ExitOnError)
	targets := fs.String("targets", "localhost:50051", "comma-separated registry endpoints")
	serviceName := fs.String("name", "", "service name")
	serviceID := fs.String("id", "", "service id")
	endpoint := fs.String("endpoint", "", "service endpoint")
	version := fs.String("version", "", "service version")
	health := fs.String("health", "HEALTH_STATUS_SERVING", "health status")
	fs.Parse(args)

	requireFlag(*serviceName, "name")
	requireFlag(*serviceID, "id")
	requireFlag(*endpoint, "endpoint")
	requireFlag(*version, "version")

	rc := newClient(*targets)
	err := rc.Register(&apiv1.ServiceRecord{
		ServiceName:  *serviceName,
		ServiceId:    *serviceID,
		Endpoint:     *endpoint,
		Version:      *version,
		HealthStatus: parseHealthStatus(*health),
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("register ok")
}

func runHeartbeat(args []string) {
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
	fs := flag.NewFlagSet("deregister", flag.ExitOnError)
	targets := fs.String("targets", "localhost:50051", "comma-separated registry endpoints")
	serviceName := fs.String("name", "", "service name")
	serviceID := fs.String("id", "", "service id")
	fs.Parse(args)

	requireFlag(*serviceName, "name")
	requireFlag(*serviceID, "id")

	rc := newClient(*targets)
	if err := rc.Deregister(*serviceName, *serviceID); err != nil {
		log.Fatal(err)
	}
	fmt.Println("deregister ok")
}

func runList(args []string) {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	targets := fs.String("targets", "localhost:50051", "comma-separated registry endpoints")
	fs.Parse(args)

	rc := newClient(*targets)
	records, err := rc.List()
	if err != nil {
		log.Fatal(err)
	}
	printJSON(records)
}

func runGet(args []string) {
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
	printJSON(records)
}

func requireFlag(value string, name string) {
	if strings.TrimSpace(value) == "" {
		log.Fatalf("-%s is required", name)
	}
}

func parseHealthStatus(value string) apiv1.HealthStatus {
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
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(data))
}
