package service

import (
	"context"
	"errors"

	"github.com/tereshed/8bitnews/internal/store"
)

// ChannelGet returns the channel profile plus the text of its format.md
// (task E1 §3).
func (s *Service) ChannelGet(ctx context.Context, req ChannelGetRequest) (ChannelView, error) {
	if req.ChannelID == "" {
		return ChannelView{}, invalidf("channel_id is required")
	}
	ch, err := s.store.GetChannel(ctx, req.ChannelID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ChannelView{}, notFoundf("channel %q not found", req.ChannelID)
		}
		return ChannelView{}, err
	}
	return ChannelView{
		ID:        ch.ID,
		Name:      ch.Name,
		Country:   ch.CountryCC,
		Language:  ch.Language,
		Timezone:  ch.Timezone,
		Platforms: ch.Platforms,
		Format:    ch.Format,
		FormatMD:  ch.FormatMD,
		Profile:   ch.Data,
	}, nil
}
