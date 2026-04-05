package session

import (
	"strings"

	"crawler-ai/internal/domain"
	apperrors "crawler-ai/internal/errors"
)

type PromptHistoryReader interface {
	ReadPersistedMessages(id string) ([]domain.TranscriptEntry, error)
}

type PromptHistoryService struct {
	reader PromptHistoryReader
}

func NewPromptHistoryService(reader PromptHistoryReader) *PromptHistoryService {
	return &PromptHistoryService{reader: reader}
}

func (s *PromptHistoryService) Messages(id string) ([]domain.TranscriptEntry, error) {
	if s == nil || s.reader == nil {
		return nil, apperrors.New("session.PromptHistoryService.Messages", apperrors.CodeStartupFailed, "prompt history service is not configured")
	}
	entries, err := s.reader.ReadPersistedMessages(id)
	if err != nil {
		return nil, err
	}
	return cloneTranscript(entries), nil
}

func (s *PromptHistoryService) PromptHistory(id string) ([]domain.TranscriptEntry, error) {
	entries, err := s.Messages(id)
	if err != nil {
		return nil, err
	}
	filtered := make([]domain.TranscriptEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Kind == domain.TranscriptReasoning {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(entry.Metadata["status"]), "in_progress") {
			continue
		}
		filtered = append(filtered, cloneTranscript([]domain.TranscriptEntry{entry})...)
	}
	return filtered, nil
}
