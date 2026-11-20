package model

// CreateTripResult distinguishes a new trip from a saved idempotent result.
// Created controls the existing HTTP contract: 201 for creation, 200 for reuse.
type CreateTripResult struct {
	Trip    Trip
	Created bool
}

// FinishTripResult reports whether the conditional active-trip update succeeded.
type FinishTripResult struct {
	Trip    Trip
	Updated bool
}
