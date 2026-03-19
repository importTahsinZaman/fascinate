package runtime

import (
	"context"
	"errors"
)

var ErrMachineNotFound = errors.New("machine not found")

type Manager interface {
	HealthCheck(context.Context) error
	ListMachines(context.Context) ([]Machine, error)
	GetMachine(context.Context, string) (Machine, error)
	CreateMachine(context.Context, CreateMachineRequest) (Machine, error)
	DeleteMachine(context.Context, string) error
	CloneMachine(context.Context, CloneMachineRequest) (Machine, error)
}

type Machine struct {
	Name      string   `json:"name"`
	Type      string   `json:"type"`
	State     string   `json:"state"`
	CPU       string   `json:"cpu,omitempty"`
	Memory    string   `json:"memory,omitempty"`
	Disk      string   `json:"disk,omitempty"`
	IPv4      []string `json:"ipv4"`
	IPv6      []string `json:"ipv6"`
	GuestUser string   `json:"guest_user,omitempty"`
}

type CreateMachineRequest struct {
	Name         string
	Image        string
	CPU          string
	Memory       string
	RootDiskSize string
	PrimaryPort  int
}

type CloneMachineRequest struct {
	SourceName   string
	TargetName   string
	RootDiskSize string
}
