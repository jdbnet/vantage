package model

import "encoding/json"

type Protocol string

const (
	ProtocolSSH Protocol = "ssh"
	ProtocolVNC Protocol = "vnc"
	ProtocolRDP Protocol = "rdp"
)

type AuthType string

const (
	AuthPassword  AuthType = "password"
	AuthPublicKey AuthType = "publickey"
)

type Folder struct {
	ID        string  `json:"id"`
	ParentID  *string `json:"parent_id"`
	Label     string  `json:"label"`
	CreatedAt string  `json:"created_at,omitempty"`
	UpdatedAt string  `json:"updated_at,omitempty"`
}

type Host struct {
	ID                string   `json:"id"`
	FolderID          *string  `json:"folder_id"`
	Label             string   `json:"label"`
	Hostname          string   `json:"hostname"`
	Port              int      `json:"port"`
	Protocol          Protocol `json:"protocol"`
	IdentityID        *string  `json:"identity_id"`
	JumpHostID        *string  `json:"jump_host_id"`
	JumpHostLabel     *string  `json:"jump_host_label,omitempty"`
	IdentityLabel     string   `json:"identity_label"`
	IdentityAuthType  string   `json:"identity_auth_type"`
	FolderLabel       *string  `json:"folder_label,omitempty"`
	LastConnectedAt   *string  `json:"last_connected_at,omitempty"`
	Tags              []string `json:"tags,omitempty"`
	HasInlineIdentity bool     `json:"has_inline_identity,omitempty"`
	CreatedAt         string   `json:"created_at,omitempty"`
	UpdatedAt         string   `json:"updated_at,omitempty"`
}

type Identity struct {
	ID        string   `json:"id"`
	Label     string   `json:"label"`
	AuthType  AuthType `json:"auth_type"`
	CreatedAt string   `json:"created_at,omitempty"`
	UpdatedAt string   `json:"updated_at,omitempty"`
}

type IdentitySecret struct {
	Username   string `json:"username"`
	Password   string `json:"password,omitempty"`
	PrivateKey string `json:"private_key,omitempty"`
	Passphrase string `json:"key_passphrase,omitempty"`
	Domain     string `json:"domain,omitempty"`
}

type Snippet struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Command   string `json:"command"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

type ConnectionAudit struct {
	ID              int64   `json:"id"`
	HostID          *string `json:"host_id"`
	HostLabel       string  `json:"host_label"`
	Hostname        string  `json:"hostname"`
	Port            int     `json:"port"`
	Protocol        string  `json:"protocol,omitempty"`
	JumpHostID      *string `json:"jump_host_id"`
	StartedAt       string  `json:"started_at"`
	EndedAt         *string `json:"ended_at"`
	DurationSeconds *int    `json:"duration_seconds"`
}

type APIKey struct {
	ID         string   `json:"id"`
	Label      string   `json:"label"`
	KeyPrefix  string   `json:"key_prefix"`
	Scopes     []string `json:"scopes"`
	ExpiresAt  *string  `json:"expires_at"`
	LastUsedAt *string  `json:"last_used_at"`
	RevokedAt  *string  `json:"revoked_at"`
	CreatedAt  string   `json:"created_at"`
	Expired    bool     `json:"expired"`
	Active     bool     `json:"active"`
}

type APIKeyScopeDef struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

type Tag struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

type KnownHost struct {
	ID          string `json:"id"`
	Hostname    string `json:"hostname"`
	Port        int    `json:"port"`
	KeyType     string `json:"key_type"`
	Fingerprint string `json:"fingerprint"`
	PublicKey   string `json:"public_key"`
	CreatedAt   string `json:"created_at,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

type Settings struct {
	ListenAddr         string `json:"listen_addr"`
	GuacdAddr          string `json:"guacd_addr"`
	SharedFilesDir     string `json:"shared_files_dir"`
	GuacdDrivePath     string `json:"guacd_drive_path"`
	TerminalTheme      string `json:"terminal_theme"`
	TerminalFontFamily string `json:"terminal_font_family"`
	TerminalFontSize   int    `json:"terminal_font_size"`
	DisplayColorDepth  int    `json:"display_color_depth"`
	DisplayWidth       int    `json:"display_width"`
	DisplayHeight      int    `json:"display_height"`
	AccentColor        string `json:"accent_color"`
	SyncURL            string `json:"sync_url"`
	SyncAPIKeySet      bool   `json:"sync_api_key_set"`
	AuditLogEnabled    bool   `json:"audit_log_enabled"`
	ReplicaID          string `json:"replica_id"`
	Mode               string `json:"mode"`
}

type BrowseResult struct {
	Breadcrumb   []Folder `json:"breadcrumb"`
	Folders      []Folder `json:"folders"`
	Hosts        []Host   `json:"hosts"`
	SearchActive bool     `json:"search_active"`
}

type ChangeOp struct {
	Seq       int64           `json:"seq"`
	Entity    string          `json:"entity"`
	EntityID  string          `json:"entity_id"`
	Op        string          `json:"op"`
	UpdatedAt string          `json:"row_updated_at"`
	Origin    string          `json:"origin_replica_id"`
	Payload   json.RawMessage `json:"payload"`
}
