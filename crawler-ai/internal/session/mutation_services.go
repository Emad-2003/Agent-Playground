package session

import (
	"crawler-ai/internal/domain"
	apperrors "crawler-ai/internal/errors"
)

type transcriptMutationStore interface {
	Get(id string) (Session, bool)
	AppendTranscript(sessionID string, entry domain.TranscriptEntry) error
	UpdateTranscript(sessionID string, entry domain.TranscriptEntry) error
	ReplaceTranscript(sessionID string, entries []domain.TranscriptEntry) error
	Save(id string) error
}

type TranscriptMutationService struct {
	store transcriptMutationStore
}

func NewTranscriptMutationService(store transcriptMutationStore) *TranscriptMutationService {
	return &TranscriptMutationService{store: store}
}

func (s *TranscriptMutationService) Session(sessionID string) (Session, bool) {
	if s == nil || s.store == nil {
		return Session{}, false
	}
	return s.store.Get(sessionID)
}

func (s *TranscriptMutationService) Append(sessionID string, entry domain.TranscriptEntry) error {
	if s == nil || s.store == nil {
		return apperrors.New("session.TranscriptMutationService.Append", apperrors.CodeStartupFailed, "transcript mutation service is not configured")
	}
	if err := s.store.AppendTranscript(sessionID, entry); err != nil {
		return err
	}
	return s.store.Save(sessionID)
}

func (s *TranscriptMutationService) Update(sessionID string, entry domain.TranscriptEntry) error {
	if s == nil || s.store == nil {
		return apperrors.New("session.TranscriptMutationService.Update", apperrors.CodeStartupFailed, "transcript mutation service is not configured")
	}
	if err := s.store.UpdateTranscript(sessionID, entry); err != nil {
		return err
	}
	return s.store.Save(sessionID)
}

func (s *TranscriptMutationService) Replace(sessionID string, entries []domain.TranscriptEntry) error {
	if s == nil || s.store == nil {
		return apperrors.New("session.TranscriptMutationService.Replace", apperrors.CodeStartupFailed, "transcript mutation service is not configured")
	}
	if err := s.store.ReplaceTranscript(sessionID, entries); err != nil {
		return err
	}
	return s.store.Save(sessionID)
}

type taskMutationStore interface {
	SetTasks(sessionID string, tasks []domain.Task) error
	Save(id string) error
}

type TaskMutationService struct {
	store taskMutationStore
}

func NewTaskMutationService(store taskMutationStore) *TaskMutationService {
	return &TaskMutationService{store: store}
}

func (s *TaskMutationService) Set(sessionID string, tasks []domain.Task) error {
	if s == nil || s.store == nil {
		return apperrors.New("session.TaskMutationService.Set", apperrors.CodeStartupFailed, "task mutation service is not configured")
	}
	if err := s.store.SetTasks(sessionID, tasks); err != nil {
		return err
	}
	return s.store.Save(sessionID)
}

type usageMutationStore interface {
	RecordUsage(sessionID string, update UsageUpdate) (UsageTotals, error)
	Save(id string) error
}

type UsageMutationService struct {
	store usageMutationStore
}

func NewUsageMutationService(store usageMutationStore) *UsageMutationService {
	return &UsageMutationService{store: store}
}

func (s *UsageMutationService) Record(sessionID string, update UsageUpdate) (UsageTotals, error) {
	if s == nil || s.store == nil {
		return UsageTotals{}, apperrors.New("session.UsageMutationService.Record", apperrors.CodeStartupFailed, "usage mutation service is not configured")
	}
	totals, err := s.store.RecordUsage(sessionID, update)
	if err != nil {
		return UsageTotals{}, err
	}
	if err := s.store.Save(sessionID); err != nil {
		return UsageTotals{}, err
	}
	return totals, nil
}

type fileMutationStore interface {
	TrackFile(sessionID string, file FileRecord) error
	Save(id string) error
}

type FileMutationService struct {
	store fileMutationStore
}

func NewFileMutationService(store fileMutationStore) *FileMutationService {
	return &FileMutationService{store: store}
}

func (s *FileMutationService) Track(sessionID string, file FileRecord) error {
	return s.TrackAll(sessionID, []FileRecord{file})
}

func (s *FileMutationService) TrackAll(sessionID string, files []FileRecord) error {
	if s == nil || s.store == nil {
		return apperrors.New("session.FileMutationService.TrackAll", apperrors.CodeStartupFailed, "file mutation service is not configured")
	}
	for _, file := range files {
		if err := s.store.TrackFile(sessionID, file); err != nil {
			return err
		}
	}
	return s.store.Save(sessionID)
}

type lifecycleStore interface {
	Get(id string) (Session, bool)
	CreateChild(id, workspaceRoot, parentSessionID string) (Session, error)
	Delete(id string) error
	Save(id string) error
}

type LifecycleService struct {
	store lifecycleStore
}

func NewLifecycleService(store lifecycleStore) *LifecycleService {
	return &LifecycleService{store: store}
}

func (s *LifecycleService) CreateChild(parentSessionID, childSessionID string) (Session, error) {
	if s == nil || s.store == nil {
		return Session{}, apperrors.New("session.LifecycleService.CreateChild", apperrors.CodeStartupFailed, "session lifecycle service is not configured")
	}
	parent, ok := s.store.Get(parentSessionID)
	if !ok {
		return Session{}, apperrors.New("session.LifecycleService.CreateChild", apperrors.CodeInvalidArgument, "parent session not found")
	}
	child, err := s.store.CreateChild(childSessionID, parent.WorkspaceRoot, parentSessionID)
	if err != nil {
		return Session{}, err
	}
	if err := s.store.Save(child.ID); err != nil {
		return Session{}, err
	}
	return child, nil
}

func (s *LifecycleService) Delete(sessionID string) error {
	if s == nil || s.store == nil {
		return apperrors.New("session.LifecycleService.Delete", apperrors.CodeStartupFailed, "session lifecycle service is not configured")
	}
	return s.store.Delete(sessionID)
}
