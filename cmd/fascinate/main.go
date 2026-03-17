package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"fascinate/internal/app"
	"fascinate/internal/config"
	"fascinate/internal/runtime/incus"
)

func main() {
	log.SetFlags(0)

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

	log.Printf("fascinate listening on %s", cfg.HTTPAddr)
	return controlPlane.Run(ctx)
}

func runRuntimeMachines(ctx context.Context, cfg config.Config) error {
	runtimeClient := incus.NewCLI(cfg.IncusBinary)
	machines, err := runtimeClient.ListMachines(ctx)
	if err != nil {
		return err
	}

	for _, machine := range machines {
		fmt.Printf("%s\t%s\t%s\n", machine.Name, machine.Type, machine.State)
	}

	return nil
}
