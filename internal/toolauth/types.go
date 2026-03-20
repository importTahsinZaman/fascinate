package toolauth

import "context"

type StorageMode string

const (
	StorageModeSessionState       StorageMode = "session_state"
	StorageModeSecretMaterial     StorageMode = "secret_material"
	StorageModeProviderCredential StorageMode = "provider_credentials"
)

type ProfileKey struct {
	UserID       string `json:"user_id"`
	ToolID       string `json:"tool_id"`
	AuthMethodID string `json:"auth_method_id"`
}

type Profile struct {
	Key              ProfileKey  `json:"key"`
	StorageMode      StorageMode `json:"storage_mode"`
	Version          int         `json:"version"`
	BundleSHA256     string      `json:"bundle_sha256"`
	Empty            bool        `json:"empty"`
	UpdatedAt        string      `json:"updated_at"`
	LastCaptureAt    *string     `json:"last_capture_at,omitempty"`
	LastCaptureError *string     `json:"last_capture_error,omitempty"`
	LastRestoreAt    *string     `json:"last_restore_at,omitempty"`
	LastRestoreError *string     `json:"last_restore_error,omitempty"`
}

type Adapter interface {
	ToolID() string
	AuthMethodID() string
	StorageMode() StorageMode
}

type SessionStateAdapter interface {
	Adapter
	SessionStateSpec(guestUser string) SessionStateSpec
}

type SecretMaterialAdapter interface {
	Adapter
	SecretMaterialSpec(guestUser string) SecretMaterialSpec
}

type ProviderCredentialsAdapter interface {
	Adapter
	ProviderCredentialsSpec(guestUser string) ProviderCredentialsSpec
}

type SessionStateSpec struct {
	Version int                `json:"version"`
	Roots   []SessionStateRoot `json:"roots"`
}

type SecretMaterialSpec struct {
	Version int          `json:"version"`
	Files   []SecretFile `json:"files,omitempty"`
	Env     []SecretEnv  `json:"env,omitempty"`
}

type ProviderCredentialsSpec struct {
	Version int      `json:"version"`
	Roots   []string `json:"roots,omitempty"`
}

type SessionStateRootKind string

const (
	SessionStateRootKindDirectory SessionStateRootKind = "directory"
	SessionStateRootKindFile      SessionStateRootKind = "file"
)

type SessionStateRoot struct {
	Path             string               `json:"path"`
	Kind             SessionStateRootKind `json:"kind,omitempty"`
	Owner            string               `json:"owner,omitempty"`
	Group            string               `json:"group,omitempty"`
	DirectoryMode    int                  `json:"directory_mode"`
	ExcludeBaseNames []string             `json:"exclude_base_names,omitempty"`
}

type SecretFile struct {
	Path          string `json:"path"`
	Owner         string `json:"owner,omitempty"`
	Group         string `json:"group,omitempty"`
	FileMode      int    `json:"file_mode"`
	SecretRefName string `json:"secret_ref_name"`
}

type SecretEnv struct {
	Name          string `json:"name"`
	SecretRefName string `json:"secret_ref_name"`
}

type GuestTransport interface {
	CaptureSessionState(context.Context, string, SessionStateSpec) ([]byte, error)
	RestoreSessionState(context.Context, string, SessionStateSpec, []byte) error
}
