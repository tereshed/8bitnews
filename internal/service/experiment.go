package service

import (
	"context"
	"errors"

	"github.com/tereshed/8bitnews/internal/store"
)

// defaultArmID is the arm every development gets while experiments are not
// implemented yet (E6).
const defaultArmID = "default"

// ExperimentAssign assigns the variant parameters for a development. E1
// has no experiment logic yet, so it always returns the channel's default
// arm with empty params, but records the assignment so E6 can build real
// randomization on top of the same table without changing this contract
// (docs/architecture.md §9, task E1 §3).
func (s *Service) ExperimentAssign(ctx context.Context, req ExperimentAssignRequest) (ExperimentAssignResponse, error) {
	if req.ChannelID == "" {
		return ExperimentAssignResponse{}, invalidf("channel_id is required")
	}
	if req.DevelopmentID == "" {
		return ExperimentAssignResponse{}, invalidf("development_id is required")
	}
	id, err := parseID("development_id", req.DevelopmentID)
	if err != nil {
		return ExperimentAssignResponse{}, err
	}
	req.DevelopmentID = id

	if _, err := s.store.GetDevelopment(ctx, req.DevelopmentID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ExperimentAssignResponse{}, notFoundf("development %q not found", req.DevelopmentID)
		}
		return ExperimentAssignResponse{}, err
	}

	params := []byte(`{}`)
	if _, err := s.store.CreateAssignment(ctx, store.NewAssignmentParams{
		DevelopmentID: req.DevelopmentID,
		ChannelID:     req.ChannelID,
		ExperimentID:  nil,
		ArmID:         defaultArmID,
		Params:        params,
	}); err != nil {
		return ExperimentAssignResponse{}, err
	}

	return ExperimentAssignResponse{ExperimentID: nil, ArmID: defaultArmID, Params: params}, nil
}
