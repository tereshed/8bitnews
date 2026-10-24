package store

import (
	"context"
	"encoding/json"
	"fmt"
)

// NewAssignmentParams is the input to CreateAssignment.
type NewAssignmentParams struct {
	DevelopmentID string
	ChannelID     string
	ExperimentID  *string
	ArmID         string
	Params        json.RawMessage
}

// CreateAssignment records the experiment arm given to a development. In E1
// there are no experiments yet, so ExperimentID is always nil and ArmID is
// always "default" (docs/architecture.md §9); the contract is final so E6
// can fill it in without changing callers.
func (s *Store) CreateAssignment(ctx context.Context, p NewAssignmentParams) (Assignment, error) {
	var a Assignment
	err := s.pool.QueryRow(ctx, `
		INSERT INTO assignments (development_id, channel_id, experiment_id, arm_id, params)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, development_id, channel_id, experiment_id, arm_id, params, created_at
	`, p.DevelopmentID, p.ChannelID, p.ExperimentID, p.ArmID, nonNilJSON(p.Params)).Scan(
		&a.ID, &a.DevelopmentID, &a.ChannelID, &a.ExperimentID, &a.ArmID, &a.Params, &a.CreatedAt,
	)
	if err != nil {
		return Assignment{}, fmt.Errorf("creating assignment: %w", err)
	}
	return a, nil
}
