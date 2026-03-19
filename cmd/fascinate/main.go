package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"fascinate/internal/app"
	"fascinate/internal/config"
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
	case "seed-ssh-key":
		if err := runSeedSSHKey(ctx, cfg, os.Args[2:]); err != nil {
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

	log.Printf("fascinate listening on %s", cfg.HTTPAddr)
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

func runSeedSSHKey(ctx context.Context, cfg config.Config, args []string) error {
	flags := flag.NewFlagSet("seed-ssh-key", flag.ContinueOnError)

	email := flags.String("email", "", "user email")
	name := flags.String("name", "", "ssh key name")
	publicKeyFile := flags.String("public-key-file", "", "path to OpenSSH public key")
	if err := flags.Parse(args); err != nil {
		return err
	}

	if *email == "" || *name == "" || *publicKeyFile == "" {
		return fmt.Errorf("usage: fascinate seed-ssh-key --email <email> --name <name> --public-key-file <path>")
	}

	publicKeyPath := *publicKeyFile
	if !filepath.IsAbs(publicKeyPath) {
		publicKeyPath = filepath.Clean(publicKeyPath)
	}

	body, err := os.ReadFile(publicKeyPath)
	if err != nil {
		return err
	}

	record, err := app.SeedSSHKey(ctx, cfg, *email, *name, string(body))
	if err != nil {
		return err
	}

	fmt.Printf("seeded ssh key %s for %s (%s)\n", record.Name, record.UserEmail, record.Fingerprint)
	return nil
}
