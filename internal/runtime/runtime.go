package runtime

import (
	"context"
	"errors"
)

var ErrMachineNotFound = errors.New("machine not found")
var ErrSnapshotNotFound = errors.New("snapshot not found")

type Manager interface {
	HealthCheck(context.Context) error
	ListMachines(context.Context) ([]Machine, error)
	GetMachine(context.Context, string) (Machine, error)
	CreateMachine(context.Context, CreateMachineRequest) (Machine, error)
	DeleteMachine(context.Context, string) error
	CloneMachine(context.Context, CloneMachineRequest) (Machine, error)
	ListSnapshots(context.Context) ([]Snapshot, error)
	GetSnapshot(context.Context, string) (Snapshot, error)
	CreateSnapshot(context.Context, CreateSnapshotRequest) (Snapshot, error)
	DeleteSnapshot(context.Context, string) error
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
	AppHost   string   `json:"app_host,omitempty"`
	AppPort   int      `json:"app_port,omitempty"`
	SSHHost   string   `json:"ssh_host,omitempty"`
	SSHPort   int      `json:"ssh_port,omitempty"`
}

type CreateMachineRequest struct {
	Name         string
	Image        string
	Snapshot     string
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

type Snapshot struct {
	Name              string `json:"name"`
	SourceMachineName string `json:"source_machine_name,omitempty"`
	State             string `json:"state"`
	ArtifactDir       string `json:"artifact_dir,omitempty"`
	DiskSizeBytes     int64  `json:"disk_size_bytes,omitempty"`
	MemorySizeBytes   int64  `json:"memory_size_bytes,omitempty"`
	RuntimeVersion    string `json:"runtime_version,omitempty"`
	FirmwareVersion   string `json:"firmware_version,omitempty"`
	CreatedAt         string `json:"created_at,omitempty"`
}

type CreateSnapshotRequest struct {
	MachineName   string
	SnapshotName  string
	ArtifactDir   string
}
