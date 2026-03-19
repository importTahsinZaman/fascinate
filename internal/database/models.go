package database

type User struct {
	ID                  string  `json:"id"`
	Email               string  `json:"email"`
	IsAdmin             bool    `json:"is_admin"`
	TutorialCompletedAt *string `json:"tutorial_completed_at,omitempty"`
	CreatedAt           string  `json:"created_at"`
}

type MachineRecord struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	OwnerUserID string  `json:"owner_user_id"`
	OwnerEmail  string  `json:"owner_email"`
	RuntimeName string  `json:"runtime_name"`
	State       string  `json:"state"`
	PrimaryPort int     `json:"primary_port"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
	DeletedAt   *string `json:"deleted_at,omitempty"`
}

type CreateMachineParams struct {
	ID          string
	Name        string
	OwnerUserID string
	RuntimeName string
	State       string
	PrimaryPort int
}

type SSHKeyRecord struct {
	ID          string `json:"id"`
	UserID      string `json:"user_id"`
	UserEmail   string `json:"user_email"`
	Name        string `json:"name"`
	PublicKey   string `json:"public_key"`
	Fingerprint string `json:"fingerprint"`
	CreatedAt   string `json:"created_at"`
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
