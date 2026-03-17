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

var ErrMachineNotFound = errors.New("machine not found")

type Runtime interface {
	HealthCheck(context.Context) error
	ListMachines(context.Context) ([]Machine, error)
	GetMachine(context.Context, string) (Machine, error)
	CreateMachine(context.Context, CreateMachineRequest) (Machine, error)
	DeleteMachine(context.Context, string) error
	CloneMachine(context.Context, CloneMachineRequest) (Machine, error)
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

type CreateMachineRequest struct {
	Name        string
	Image       string
	StoragePool string
	CPU         string
	Memory      string
	PrimaryPort int
}

type CloneMachineRequest struct {
	SourceName string
	TargetName string
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

func (c *CLI) GetMachine(ctx context.Context, name string) (Machine, error) {
	output, err := c.run(ctx, "list", strings.TrimSpace(name), "--format", "json")
	if err != nil {
		return Machine{}, err
	}

	var instances []rawInstance
	if err := json.Unmarshal(output, &instances); err != nil {
		return Machine{}, fmt.Errorf("decode incus list output: %w", err)
	}
	if len(instances) == 0 {
		return Machine{}, ErrMachineNotFound
	}

	return machineFromRaw(instances[0]), nil
}

func (c *CLI) CreateMachine(ctx context.Context, req CreateMachineRequest) (Machine, error) {
	name := strings.TrimSpace(req.Name)
	image := strings.TrimSpace(req.Image)
	if name == "" || image == "" {
		return Machine{}, fmt.Errorf("machine name and image are required")
	}

	args := []string{"init", image, name}
	if pool := strings.TrimSpace(req.StoragePool); pool != "" {
		args = append(args, "-s", pool)
	}
	if _, err := c.run(ctx, args...); err != nil {
		return Machine{}, err
	}

	cleanup := true
	defer func() {
		if !cleanup {
			return
		}
		_, _ = c.run(context.Background(), "delete", "--force", name)
	}()

	configValues := map[string]string{
		"boot.autostart":                          "true",
		"security.nesting":                        "true",
		"security.syscalls.intercept.mknod":       "true",
		"security.syscalls.intercept.setxattr":    "true",
		"linux.kernel_modules":                    "overlay,br_netfilter",
		"user.fascinate.primary_port":             fmt.Sprintf("%d", req.PrimaryPort),
		"user.fascinate.managed_by_control_plane": "true",
	}
	if value := strings.TrimSpace(req.CPU); value != "" {
		configValues["limits.cpu"] = value
	}
	if value := strings.TrimSpace(req.Memory); value != "" {
		configValues["limits.memory"] = value
	}

	for key, value := range configValues {
		if _, err := c.run(ctx, "config", "set", name, key, value); err != nil {
			return Machine{}, err
		}
	}

	if _, err := c.run(ctx, "start", name); err != nil {
		return Machine{}, err
	}

	machine, err := c.GetMachine(ctx, name)
	if err != nil {
		return Machine{}, err
	}

	cleanup = false
	return machine, nil
}

func (c *CLI) DeleteMachine(ctx context.Context, name string) error {
	_, err := c.run(ctx, "delete", "--force", strings.TrimSpace(name))
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return ErrMachineNotFound
		}
		return err
	}

	return nil
}

func (c *CLI) CloneMachine(ctx context.Context, req CloneMachineRequest) (Machine, error) {
	source := strings.TrimSpace(req.SourceName)
	target := strings.TrimSpace(req.TargetName)
	if source == "" || target == "" {
		return Machine{}, fmt.Errorf("source and target names are required")
	}

	if _, err := c.run(ctx, "copy", source, target); err != nil {
		return Machine{}, err
	}

	cleanup := true
	defer func() {
		if !cleanup {
			return
		}
		_, _ = c.run(context.Background(), "delete", "--force", target)
	}()

	if _, err := c.run(ctx, "start", target); err != nil {
		return Machine{}, err
	}

	machine, err := c.GetMachine(ctx, target)
	if err != nil {
		return Machine{}, err
	}

	cleanup = false
	return machine, nil
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
			stderrValue := strings.TrimSpace(stderr.String())
			if strings.Contains(stderrValue, "not found") {
				return nil, fmt.Errorf("%w: %s", ErrMachineNotFound, stderrValue)
			}
			return nil, fmt.Errorf("%w: %s", err, stderrValue)
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
