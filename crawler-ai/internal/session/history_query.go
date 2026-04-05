package session

import "crawler-ai/internal/domain"

type HistoryQueryReader interface {
	ReadPersistedMessages(id string) ([]domain.TranscriptEntry, error)
	ReadPersistedFiles(id string) ([]FileRecord, error)
}

type HistoryQueryService struct {
	prompt *PromptHistoryService
	files  *FileHistoryService
}

func NewHistoryQueryService(reader HistoryQueryReader) *HistoryQueryService {
	return &HistoryQueryService{
		prompt: NewPromptHistoryService(reader),
		files:  NewFileHistoryService(reader),
	}
}

func (s *HistoryQueryService) Messages(id string) ([]domain.TranscriptEntry, error) {
	return s.prompt.Messages(id)
}

func (s *HistoryQueryService) PromptHistory(id string) ([]domain.TranscriptEntry, error) {
	return s.prompt.PromptHistory(id)
}

func (s *HistoryQueryService) Files(id string) ([]FileRecord, error) {
	return s.files.Files(id)
}

func (s *HistoryQueryService) FileHistory(id string) (SessionFileHistory, error) {
	return s.files.FileHistory(id)
}

func (s *HistoryQueryService) FilteredFileHistory(id string, options FileHistoryOptions) (SessionFileHistory, error) {
	return s.files.FilteredFileHistory(id, options)
}
