package database

type User struct {
	ID                  string  `json:"id"`
	Email               string  `json:"email"`
	IsAdmin             bool    `json:"is_admin"`
	MaxCPU              string  `json:"max_cpu"`
	MaxMemoryBytes      int64   `json:"max_memory_bytes"`
	MaxDiskBytes        int64   `json:"max_disk_bytes"`
	MaxMachineCount     int     `json:"max_machine_count"`
	MaxSnapshotCount    int     `json:"max_snapshot_count"`
	TutorialCompletedAt *string `json:"tutorial_completed_at,omitempty"`
	CreatedAt           string  `json:"created_at"`
}

type MachineRecord struct {
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	OwnerUserID      string  `json:"owner_user_id"`
	OwnerEmail       string  `json:"owner_email"`
	HostID           *string `json:"host_id,omitempty"`
	RuntimeName      string  `json:"runtime_name"`
	SourceSnapshotID *string `json:"source_snapshot_id,omitempty"`
	State            string  `json:"state"`
	CPU              string  `json:"cpu"`
	MemoryBytes      int64   `json:"memory_bytes"`
	DiskBytes        int64   `json:"disk_bytes"`
	DiskUsageBytes   int64   `json:"disk_usage_bytes"`
	PrimaryPort      int     `json:"primary_port"`
	CreatedAt        string  `json:"created_at"`
	UpdatedAt        string  `json:"updated_at"`
	DeletedAt        *string `json:"deleted_at,omitempty"`
}

type CreateMachineParams struct {
	ID               string
	Name             string
	OwnerUserID      string
	HostID           *string
	RuntimeName      string
	SourceSnapshotID *string
	State            string
	CPU              string
	MemoryBytes      int64
	DiskBytes        int64
	DiskUsageBytes   int64
	PrimaryPort      int
}

type SnapshotRecord struct {
	ID                string  `json:"id"`
	Name              string  `json:"name"`
	OwnerUserID       string  `json:"owner_user_id"`
	OwnerEmail        string  `json:"owner_email"`
	HostID            *string `json:"host_id,omitempty"`
	SourceMachineID   *string `json:"source_machine_id,omitempty"`
	SourceMachineName *string `json:"source_machine_name,omitempty"`
	RuntimeName       string  `json:"runtime_name"`
	State             string  `json:"state"`
	CPU               string  `json:"cpu"`
	MemoryBytes       int64   `json:"memory_bytes"`
	DiskBytes         int64   `json:"disk_bytes"`
	ArtifactDir       string  `json:"artifact_dir"`
	DiskSizeBytes     int64   `json:"disk_size_bytes"`
	MemorySizeBytes   int64   `json:"memory_size_bytes"`
	RuntimeVersion    string  `json:"runtime_version"`
	FirmwareVersion   string  `json:"firmware_version"`
	CreatedAt         string  `json:"created_at"`
	UpdatedAt         string  `json:"updated_at"`
	DeletedAt         *string `json:"deleted_at,omitempty"`
}

type CreateSnapshotParams struct {
	ID              string
	Name            string
	OwnerUserID     string
	HostID          *string
	SourceMachineID *string
	RuntimeName     string
	State           string
	CPU             string
	MemoryBytes     int64
	DiskBytes       int64
	ArtifactDir     string
	DiskSizeBytes   int64
	MemorySizeBytes int64
	RuntimeVersion  string
	FirmwareVersion string
}

type UserBudgetDefaults struct {
	MaxCPU           string
	MaxMemoryBytes   int64
	MaxDiskBytes     int64
	MaxMachineCount  int
	MaxSnapshotCount int
}

type EmailCodeRecord struct {
	ID         string  `json:"id"`
	UserID     *string `json:"user_id,omitempty"`
	Email      string  `json:"email"`
	Purpose    string  `json:"purpose"`
	CodeHash   string  `json:"code_hash"`
	ExpiresAt  string  `json:"expires_at"`
	ConsumedAt *string `json:"consumed_at,omitempty"`
	CreatedAt  string  `json:"created_at"`
}

type EventRecord struct {
	ID          string  `json:"id"`
	ActorUserID *string `json:"actor_user_id,omitempty"`
	MachineID   *string `json:"machine_id,omitempty"`
	Kind        string  `json:"kind"`
	PayloadJSON string  `json:"payload_json"`
	CreatedAt   string  `json:"created_at"`
}

type HostRecord struct {
	ID                   string  `json:"id"`
	Name                 string  `json:"name"`
	Region               string  `json:"region"`
	Role                 string  `json:"role"`
	Status               string  `json:"status"`
	LabelsJSON           string  `json:"labels_json"`
	CapabilitiesJSON     string  `json:"capabilities_json"`
	RuntimeVersion       string  `json:"runtime_version"`
	HeartbeatAt          *string `json:"heartbeat_at,omitempty"`
	TotalCPU             int     `json:"total_cpu"`
	AllocatedCPU         int     `json:"allocated_cpu"`
	TotalMemoryBytes     int64   `json:"total_memory_bytes"`
	AllocatedMemoryBytes int64   `json:"allocated_memory_bytes"`
	TotalDiskBytes       int64   `json:"total_disk_bytes"`
	AllocatedDiskBytes   int64   `json:"allocated_disk_bytes"`
	AvailableDiskBytes   int64   `json:"available_disk_bytes"`
	MachineCount         int     `json:"machine_count"`
	SnapshotCount        int     `json:"snapshot_count"`
	LastError            *string `json:"last_error,omitempty"`
	CreatedAt            string  `json:"created_at"`
	UpdatedAt            string  `json:"updated_at"`
}

type UpsertHostParams struct {
	ID               string
	Name             string
	Region           string
	Role             string
	Status           string
	LabelsJSON       string
	CapabilitiesJSON string
	RuntimeVersion   string
}

type UpdateHostHeartbeatParams struct {
	ID                   string
	RuntimeVersion       string
	Healthy              bool
	TotalCPU             int
	AllocatedCPU         int
	TotalMemoryBytes     int64
	AllocatedMemoryBytes int64
	TotalDiskBytes       int64
	AllocatedDiskBytes   int64
	AvailableDiskBytes   int64
	MachineCount         int
	SnapshotCount        int
	LastError            *string
}

type CreateEventParams struct {
	ID          string
	ActorUserID *string
	MachineID   *string
	Kind        string
	PayloadJSON string
}

type EnvVarRecord struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	Key       string `json:"key"`
	RawValue  string `json:"raw_value"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type UpsertEnvVarParams struct {
	ID       string
	UserID   string
	Key      string
	RawValue string
}

type WebSessionRecord struct {
	ID         string  `json:"id"`
	UserID     string  `json:"user_id"`
	UserEmail  string  `json:"user_email"`
	TokenHash  string  `json:"token_hash"`
	ExpiresAt  string  `json:"expires_at"`
	LastSeenAt string  `json:"last_seen_at"`
	UserAgent  *string `json:"user_agent,omitempty"`
	IPAddress  *string `json:"ip_address,omitempty"`
	RevokedAt  *string `json:"revoked_at,omitempty"`
	CreatedAt  string  `json:"created_at"`
}

type CreateWebSessionParams struct {
	ID        string
	UserID    string
	TokenHash string
	ExpiresAt string
	UserAgent string
	IPAddress string
}

type WorkspaceLayoutRecord struct {
	ID         string `json:"id"`
	UserID     string `json:"user_id"`
	UserEmail  string `json:"user_email"`
	Name       string `json:"name"`
	LayoutJSON string `json:"layout_json"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

type UpsertWorkspaceLayoutParams struct {
	ID         string
	UserID     string
	Name       string
	LayoutJSON string
}
