package database

type User struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	IsAdmin   bool   `json:"is_admin"`
	CreatedAt string `json:"created_at"`
}

type MachineRecord struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	OwnerUserID string  `json:"owner_user_id"`
	OwnerEmail  string  `json:"owner_email"`
	IncusName   string  `json:"incus_name"`
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
	IncusName   string
	State       string
	PrimaryPort int
}
