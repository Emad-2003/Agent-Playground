package session

import apperrors "crawler-ai/internal/errors"

type UsageQueryReader interface {
	ReadPersistedSummary(id string) (SessionSummary, error)
	ReadPersistedUsage(id string) (UsageTotals, error)
}

type UsageProjection struct {
	SessionID string      `json:"session_id"`
	Usage     UsageTotals `json:"usage"`
}

type UsageQueryService struct {
	reader UsageQueryReader
}

func NewUsageQueryService(reader UsageQueryReader) *UsageQueryService {
	return &UsageQueryService{reader: reader}
}

func (s *UsageQueryService) Usage(id string) (UsageTotals, error) {
	if s == nil || s.reader == nil {
		return UsageTotals{}, apperrors.New("session.UsageQueryService.Usage", apperrors.CodeStartupFailed, "usage query service is not configured")
	}
	usage, err := s.reader.ReadPersistedUsage(id)
	if err != nil {
		return UsageTotals{}, err
	}
	return usage, nil
}

func (s *UsageQueryService) Projection(id string) (UsageProjection, error) {
	if s == nil || s.reader == nil {
		return UsageProjection{}, apperrors.New("session.UsageQueryService.Projection", apperrors.CodeStartupFailed, "usage query service is not configured")
	}
	if _, err := s.reader.ReadPersistedSummary(id); err != nil {
		return UsageProjection{}, err
	}
	usage, err := s.Usage(id)
	if err != nil {
		return UsageProjection{}, err
	}
	return UsageProjection{SessionID: id, Usage: usage}, nil
}
