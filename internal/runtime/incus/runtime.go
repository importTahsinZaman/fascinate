package incus

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

type Runtime interface {
	HealthCheck(context.Context) error
	ListMachines(context.Context) ([]Machine, error)
}

type CLI struct {
	binary string
}

type Machine struct {
	Name  string   `json:"name"`
	Type  string   `json:"type"`
	State string   `json:"state"`
	IPv4  []string `json:"ipv4"`
	IPv6  []string `json:"ipv6"`
}

func NewCLI(binary string) *CLI {
	if strings.TrimSpace(binary) == "" {
		binary = "incus"
	}

	return &CLI{binary: binary}
}

func (c *CLI) HealthCheck(ctx context.Context) error {
	_, err := c.run(ctx, "version")
	return err
}

func (c *CLI) ListMachines(ctx context.Context) ([]Machine, error) {
	output, err := c.run(ctx, "list", "--format", "json")
	if err != nil {
		return nil, err
	}

	var instances []rawInstance
	if err := json.Unmarshal(output, &instances); err != nil {
		return nil, fmt.Errorf("decode incus list output: %w", err)
	}

	machines := make([]Machine, 0, len(instances))
	for _, instance := range instances {
		machines = append(machines, machineFromRaw(instance))
	}

	sort.Slice(machines, func(i, j int) bool {
		return machines[i].Name < machines[j].Name
	})

	return machines, nil
}

func (c *CLI) run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, c.binary, args...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, fmt.Errorf("incus binary %q not found: %w", c.binary, err)
		}

		if stderr.Len() > 0 {
			return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
		}

		return nil, err
	}

	return stdout.Bytes(), nil
}

type rawInstance struct {
	Name   string    `json:"name"`
	Status string    `json:"status"`
	Type   string    `json:"type"`
	State  *rawState `json:"state"`
}

type rawState struct {
	Network map[string]rawNetwork `json:"network"`
}

type rawNetwork struct {
	Addresses []rawAddress `json:"addresses"`
}

type rawAddress struct {
	Family  string `json:"family"`
	Address string `json:"address"`
	Scope   string `json:"scope"`
}

func machineFromRaw(instance rawInstance) Machine {
	machine := Machine{
		Name:  instance.Name,
		Type:  instance.Type,
		State: instance.Status,
	}

	if instance.State == nil {
		return machine
	}

	for name, network := range instance.State.Network {
		if name == "lo" {
			continue
		}

		for _, address := range network.Addresses {
			switch address.Family {
			case "inet":
				machine.IPv4 = append(machine.IPv4, address.Address)
			case "inet6":
				if address.Scope == "link" {
					continue
				}
				machine.IPv6 = append(machine.IPv6, address.Address)
			}
		}
	}

	sort.Strings(machine.IPv4)
	sort.Strings(machine.IPv6)

	return machine
}
