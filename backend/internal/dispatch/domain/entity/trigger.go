package entity

// Trigger says why an attempt happens.
type Trigger string

// Attempt triggers. Manual is a replay requested by the user (US-007).
const (
	TriggerAutomatic Trigger = "automatic"
	TriggerManual    Trigger = "manual"
)
