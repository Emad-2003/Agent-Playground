package session

import (
	"sort"
	"strings"

	apperrors "crawler-ai/internal/errors"
)

func (s *QueryService) ListRootSummaries() ([]SessionSummary, error) {
	summaries, err := s.ListSummaries()
	if err != nil {
		return nil, err
	}
	roots := make([]SessionSummary, 0, len(summaries))
	for _, summary := range summaries {
		if strings.TrimSpace(summary.ParentSessionID) != "" {
			continue
		}
		roots = append(roots, summary)
	}
	return roots, nil
}

func (s *QueryService) ChildSummaries(parentSessionID string) ([]SessionSummary, error) {
	if s == nil || s.reader == nil {
		return nil, apperrors.New("session.QueryService.ChildSummaries", apperrors.CodeStartupFailed, "persisted query service is not configured")
	}
	summaries, err := s.ListSummaries()
	if err != nil {
		return nil, err
	}
	children := make([]SessionSummary, 0)
	for _, summary := range summaries {
		if summary.ParentSessionID != parentSessionID {
			continue
		}
		children = append(children, summary)
	}
	sort.Slice(children, func(i, j int) bool {
		if children[i].UpdatedAt.Equal(children[j].UpdatedAt) {
			if children[i].CreatedAt.Equal(children[j].CreatedAt) {
				return children[i].ID < children[j].ID
			}
			return children[i].CreatedAt.After(children[j].CreatedAt)
		}
		return children[i].UpdatedAt.After(children[j].UpdatedAt)
	})
	return children, nil
}
