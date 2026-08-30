package deploy

import "time"

// EnvironmentType is the kind of runtime an environment provides. MVP has
// exactly two; staging is deferred (DESIGN.md §12).
type EnvironmentType string

const (
	EnvironmentPreview    EnvironmentType = "preview"
	EnvironmentProduction EnvironmentType = "production"
)

// Valid reports whether t is a known environment type.
func (t EnvironmentType) Valid() bool {
	switch t {
	case EnvironmentPreview, EnvironmentProduction:
		return true
	default:
		return false
	}
}

// Environment is one isolated runtime of a project — an application container,
// its Postgres database, its environment variables and its HTTPS endpoint
// (DESIGN.md §12). A project has at most one environment of each type, which the
// unique index enforces.
type Environment struct {
	ID        uint            `gorm:"primaryKey"`
	ProjectID uint            `gorm:"not null;uniqueIndex:uidx_project_env_type,priority:1"`
	Type      EnvironmentType `gorm:"size:20;not null;uniqueIndex:uidx_project_env_type,priority:2"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Domain is a hostname that routes to an environment's HTTPS endpoint. A
// hostname routes to exactly one environment, which the unique index enforces.
type Domain struct {
	ID            uint   `gorm:"primaryKey"`
	EnvironmentID uint   `gorm:"not null;index"`
	Hostname      string `gorm:"size:253;not null;uniqueIndex"`
	// Verified records whether ownership of the hostname has been proven before
	// traffic is routed to it. The verification flow itself is later work.
	Verified  bool `gorm:"not null;default:false"`
	CreatedAt time.Time
	UpdatedAt time.Time
}
