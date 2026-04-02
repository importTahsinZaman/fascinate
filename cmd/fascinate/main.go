package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"fascinate/internal/app"
	"fascinate/internal/config"
	"fascinate/internal/netnsforward"
	"fascinate/internal/runtime/cloudhypervisor"
)

func main() {
	log.SetFlags(0)

	envFile := os.Getenv("FASCINATE_ENV_FILE")
	if strings.TrimSpace(envFile) == "" {
		envFile = "/etc/fascinate/fascinate.env"
	}
	if err := config.LoadEnvFile(envFile); err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg := config.Load()
	command := "serve"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}

	switch command {
	case "serve":
		if err := runServe(ctx, cfg); err != nil {
			log.Fatal(err)
		}
	case "migrate":
		if err := app.RunMigrations(ctx, cfg); err != nil {
			log.Fatal(err)
		}
	case "runtime-machines":
		if err := runRuntimeMachines(ctx, cfg); err != nil {
			log.Fatal(err)
		}
	case "version":
		fmt.Println("fascinate dev")
	case "netns-forward":
		if err := runNetNSForward(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
	default:
		log.Fatalf("unknown command %q", command)
	}
}

func runServe(ctx context.Context, cfg config.Config) error {
	controlPlane, err := app.New(ctx, cfg)
	if err != nil {
		return err
	}
	defer controlPlane.Close()

	return controlPlane.Run(ctx)
}

func runRuntimeMachines(ctx context.Context, cfg config.Config) error {
	runtimeClient, err := cloudhypervisor.New(cfg)
	if err != nil {
		return err
	}
	machines, err := runtimeClient.ListMachines(ctx)
	if err != nil {
		return err
	}

	for _, machine := range machines {
		fmt.Printf("%s\t%s\t%s\n", machine.Name, machine.Type, machine.State)
	}

	return nil
}

func runNetNSForward(args []string) error {
	flags := flag.NewFlagSet("netns-forward", flag.ContinueOnError)
	namespace := flags.String("namespace", "", "linux network namespace name")
	listen := flags.String("listen", "127.0.0.1:0", "listen address")
	target := flags.String("target", "", "target address inside namespace")
	portFile := flags.String("port-file", "", "path to write the chosen listen port")
	if err := flags.Parse(args); err != nil {
		return err
	}

	if strings.TrimSpace(*namespace) == "" || strings.TrimSpace(*target) == "" || strings.TrimSpace(*portFile) == "" {
		return fmt.Errorf("usage: fascinate netns-forward --namespace <name> --target <host:port> --port-file <path> [--listen 127.0.0.1:0]")
	}

	ctx, stop := signal.NotifyContext(context.Background(), netnsforward.Signals()...)
	defer stop()

	return netnsforward.Run(ctx, netnsforward.Config{
		Namespace: *namespace,
		Listen:    *listen,
		Target:    *target,
		PortFile:  *portFile,
	})
}
