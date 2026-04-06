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
	SyncManagedEnv(context.Context, string, ManagedEnvRequest) error
	StartMachine(context.Context, string) (Machine, error)
	StopMachine(context.Context, string) (Machine, error)
	DeleteMachine(context.Context, string) error
	ForkMachine(context.Context, ForkMachineRequest) (Machine, error)
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
	MachineID    string
	Name         string
	Image        string
	Snapshot     string
	CPU          string
	Memory       string
	RootDiskSize string
	PrimaryPort  int
}

type ForkMachineRequest struct {
	MachineID    string
	SourceName   string
	TargetName   string
	RootDiskSize string
}

type ManagedEnvRequest struct {
	Entries map[string]string
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
	MachineName  string
	SnapshotName string
	ArtifactDir  string
}

type MachineDiagnostics struct {
	Machine             Machine  `json:"machine"`
	RuntimeName         string   `json:"runtime_name"`
	NamespaceName       string   `json:"namespace_name,omitempty"`
	BridgeName          string   `json:"bridge_name,omitempty"`
	TapDevice           string   `json:"tap_device,omitempty"`
	MACAddress          string   `json:"mac_address,omitempty"`
	DiskPath            string   `json:"disk_path,omitempty"`
	SeedPath            string   `json:"seed_path,omitempty"`
	LogPath             string   `json:"log_path,omitempty"`
	SocketPath          string   `json:"socket_path,omitempty"`
	RestoreDir          string   `json:"restore_dir,omitempty"`
	HostVethName        string   `json:"host_veth_name,omitempty"`
	NamespaceVethName   string   `json:"namespace_veth_name,omitempty"`
	HostVethIPv4        string   `json:"host_veth_ipv4,omitempty"`
	NamespaceVethIPv4   string   `json:"namespace_veth_ipv4,omitempty"`
	VMMProcessID        int      `json:"vmm_process_id,omitempty"`
	VMMProcessAlive     bool     `json:"vmm_process_alive"`
	AppForwardPID       int      `json:"app_forward_pid,omitempty"`
	AppForwardPort      int      `json:"app_forward_port,omitempty"`
	AppForwardAlive     bool     `json:"app_forward_alive"`
	AppForwardReachable bool     `json:"app_forward_reachable"`
	SSHForwardPID       int      `json:"ssh_forward_pid,omitempty"`
	SSHForwardPort      int      `json:"ssh_forward_port,omitempty"`
	SSHForwardAlive     bool     `json:"ssh_forward_alive"`
	SSHForwardReachable bool     `json:"ssh_forward_reachable"`
	LogTail             []string `json:"log_tail,omitempty"`
}

type SnapshotDiagnostics struct {
	Snapshot          Snapshot `json:"snapshot"`
	RuntimeName       string   `json:"runtime_name"`
	ArtifactDir       string   `json:"artifact_dir,omitempty"`
	DiskPath          string   `json:"disk_path,omitempty"`
	SeedPath          string   `json:"seed_path,omitempty"`
	RestoreDir        string   `json:"restore_dir,omitempty"`
	ArtifactDirExists bool     `json:"artifact_dir_exists"`
	DiskExists        bool     `json:"disk_exists"`
	SeedExists        bool     `json:"seed_exists"`
	RestoreDirExists  bool     `json:"restore_dir_exists"`
}
