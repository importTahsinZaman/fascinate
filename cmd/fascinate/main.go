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
	"fascinate/internal/cli"
	"fascinate/internal/config"
	"fascinate/internal/netnsforward"
	"fascinate/internal/runtime/cloudhypervisor"
)

func main() {
	log.SetFlags(0)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	command := ""
	if len(os.Args) > 1 {
		command = os.Args[1]
	}

	if !requiresServerConfig(command) {
		switch command {
		case "version":
			fmt.Println("fascinate dev")
			return
		default:
			if err := cli.Run(ctx, os.Args[1:]); err != nil {
				if exitErr, ok := err.(interface{ ExitCode() int }); ok {
					if message := strings.TrimSpace(err.Error()); message != "" {
						fmt.Fprintln(os.Stderr, message)
					}
					os.Exit(exitErr.ExitCode())
				}
				log.Fatal(err)
			}
			return
		}
	}

	envFile := os.Getenv("FASCINATE_ENV_FILE")
	if strings.TrimSpace(envFile) == "" {
		envFile = "/etc/fascinate/fascinate.env"
	}
	if err := config.LoadEnvFile(envFile); err != nil {
		log.Fatal(err)
	}

	cfg := config.Load()

	switch command {
	case "serve":
		if err := runServe(ctx, cfg); err != nil {
			log.Fatal(err)
		}
	case "image":
		if err := runImage(ctx, cfg, os.Args[2:]); err != nil {
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
	case "netns-forward":
		if err := runNetNSForward(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
	default:
		log.Fatalf("unknown command %q", command)
	}
}

func requiresServerConfig(command string) bool {
	switch strings.TrimSpace(command) {
	case "serve", "image", "migrate", "runtime-machines", "netns-forward":
		return true
	default:
		return false
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

func runImage(ctx context.Context, cfg config.Config, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: fascinate image <build|validate|promote|rollback|status> [flags]")
	}

	manager, err := cloudhypervisor.New(cfg)
	if err != nil {
		return err
	}

	switch args[0] {
	case "build":
		flags := flag.NewFlagSet("image build", flag.ContinueOnError)
		version := flags.String("version", "", "candidate image version (defaults to UTC timestamp)")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		result, err := manager.BuildImage(ctx, *version)
		if err != nil {
			return err
		}
		fmt.Printf("built candidate image %s\nartifact: %s\nmanifest: %s\n", result.Version, result.ImagePath, result.ManifestPath)
		return nil
	case "validate":
		flags := flag.NewFlagSet("image validate", flag.ContinueOnError)
		version := flags.String("version", "", "candidate or release image version")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if strings.TrimSpace(*version) == "" {
			return fmt.Errorf("usage: fascinate image validate --version <version>")
		}
		result, err := manager.ValidateImage(ctx, *version)
		if err != nil {
			return err
		}
		fmt.Printf("validated image %s\nartifact: %s\nmanifest: %s\n", result.Version, result.ImagePath, result.ManifestPath)
		return nil
	case "promote":
		flags := flag.NewFlagSet("image promote", flag.ContinueOnError)
		version := flags.String("version", "", "validated candidate image version")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if strings.TrimSpace(*version) == "" {
			return fmt.Errorf("usage: fascinate image promote --version <version>")
		}
		result, err := manager.PromoteImage(ctx, *version)
		if err != nil {
			return err
		}
		fmt.Printf("promoted image %s\nartifact: %s\nmanifest: %s\n", result.Version, result.ImagePath, result.ManifestPath)
		return nil
	case "rollback":
		flags := flag.NewFlagSet("image rollback", flag.ContinueOnError)
		version := flags.String("version", "", "release image version (defaults to previous)")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		result, err := manager.RollbackImage(ctx, *version)
		if err != nil {
			return err
		}
		fmt.Printf("rolled back to image %s\nartifact: %s\nmanifest: %s\n", result.Version, result.ImagePath, result.ManifestPath)
		return nil
	case "status":
		status, err := manager.ImageStatus(ctx)
		if err != nil {
			return err
		}
		if status.Current != nil {
			fmt.Printf("current\t%s\t%s\n", status.Current.Version, status.Current.ImagePath)
		} else {
			fmt.Println("current\t<none>")
		}
		if status.Previous != nil {
			fmt.Printf("previous\t%s\t%s\n", status.Previous.Version, status.Previous.ImagePath)
		} else {
			fmt.Println("previous\t<none>")
		}
		for _, manifest := range status.Candidates {
			fmt.Printf("candidate\t%s\t%s\n", manifest.Version, manifest.ImagePath)
		}
		for _, manifest := range status.Releases {
			fmt.Printf("release\t%s\t%s\n", manifest.Version, manifest.ImagePath)
		}
		return nil
	default:
		return fmt.Errorf("unknown image command %q", args[0])
	}
}
