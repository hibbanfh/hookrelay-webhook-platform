// Package entity holds the project entities and API key value objects with their invariants.
// It imports the standard library and domain/errors only.
package entity

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	domainerrors "hookrelay/internal/project/domain/errors"
)

// Identifier prefixes (opaque IDs carry a type prefix).
const (
	ProjectIDPrefix = "proj_"
	APIKeyIDPrefix  = "key_"
)

// DefaultProjectName names the project created for a dashboard user on first use.
const DefaultProjectName = "My project"

const (
	fieldName     = "name"
	fieldOwner    = "owner"
	maxNameLength = 100
)

// Project is a tenant: every other resource belongs to exactly one project.
type Project struct {
	ID        string
	Name      string
	CreatedAt time.Time
	// OwnerID is the identity-provider user who owns the project. "" means an API-key-only project.
	OwnerID string
}

// NewProject validates the name (trimmed, 1-100 characters) and returns a Project.
func NewProject(id, name string, now time.Time) (Project, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Project{}, &domainerrors.ValidationError{Field: fieldName, Reason: "must not be empty"}
	}
	if !utf8.ValidString(name) {
		return Project{}, &domainerrors.ValidationError{Field: fieldName, Reason: "must be valid UTF-8"}
	}
	if utf8.RuneCountInString(name) > maxNameLength {
		return Project{}, &domainerrors.ValidationError{Field: fieldName, Reason: "must be at most 100 characters"}
	}
	if strings.ContainsFunc(name, isForbiddenNameRune) {
		return Project{}, &domainerrors.ValidationError{Field: fieldName, Reason: "must not contain control characters"}
	}
	return Project{ID: id, Name: name, CreatedAt: now}, nil
}

// NewOwnedProject is NewProject for a project that belongs to one dashboard user.
func NewOwnedProject(id, ownerID, name string, now time.Time) (Project, error) {
	if strings.TrimSpace(ownerID) == "" {
		return Project{}, &domainerrors.ValidationError{Field: fieldOwner, Reason: "must not be empty"}
	}
	p, err := NewProject(id, name, now)
	if err != nil {
		return Project{}, err
	}
	p.OwnerID = ownerID
	return p, nil
}

// isForbiddenNameRune rejects Unicode control characters (Cc) and the bidirectional
// override/isolate controls (U+202A-U+202E, U+2066-U+2069), which can visually spoof a name.
func isForbiddenNameRune(r rune) bool {
	isBidiEmbedding := r >= '\u202a' && r <= '\u202e'
	isBidiIsolate := r >= '\u2066' && r <= '\u2069'
	return unicode.IsControl(r) || isBidiEmbedding || isBidiIsolate
}
