package session

import apperrors "crawler-ai/internal/errors"

type FileHistoryReader interface {
	ReadPersistedFiles(id string) ([]FileRecord, error)
}

type SessionFileHistory struct {
	SessionID         string       `json:"session_id"`
	Files             []FileRecord `json:"files,omitempty"`
	WorkspaceFiles    []FileRecord `json:"workspace_files,omitempty"`
	FetchedFiles      []FileRecord `json:"fetched_files,omitempty"`
	HistorySnapshots  []FileRecord `json:"history_snapshots,omitempty"`
	FilesMissingKinds []FileRecord `json:"files_missing_kinds,omitempty"`
}

type FileHistoryOptions struct {
	Kinds          []FileRecordKind
	IncludeUnknown bool
}

type FileHistoryService struct {
	reader FileHistoryReader
}

func NewFileHistoryService(reader FileHistoryReader) *FileHistoryService {
	return &FileHistoryService{reader: reader}
}

func (s *FileHistoryService) Files(id string) ([]FileRecord, error) {
	if s == nil || s.reader == nil {
		return nil, apperrors.New("session.FileHistoryService.Files", apperrors.CodeStartupFailed, "file history service is not configured")
	}
	files, err := s.reader.ReadPersistedFiles(id)
	if err != nil {
		return nil, err
	}
	return cloneFiles(files), nil
}

func (s *FileHistoryService) FileHistory(id string) (SessionFileHistory, error) {
	return s.FilteredFileHistory(id, FileHistoryOptions{})
}

func (s *FileHistoryService) FilteredFileHistory(id string, options FileHistoryOptions) (SessionFileHistory, error) {
	files, err := s.Files(id)
	if err != nil {
		return SessionFileHistory{}, err
	}
	view := SessionFileHistory{SessionID: id, Files: make([]FileRecord, 0, len(files))}
	allowedKinds := make(map[FileRecordKind]struct{}, len(options.Kinds))
	for _, kind := range options.Kinds {
		allowedKinds[kind] = struct{}{}
	}
	for _, file := range files {
		if !matchesFileHistoryOptions(file, allowedKinds, options.IncludeUnknown) {
			continue
		}
		view.Files = append(view.Files, cloneFileRecord(file))
		switch file.Kind {
		case FileRecordWorkspace:
			view.WorkspaceFiles = append(view.WorkspaceFiles, cloneFileRecord(file))
		case FileRecordFetched:
			view.FetchedFiles = append(view.FetchedFiles, cloneFileRecord(file))
		case FileRecordHistory:
			view.HistorySnapshots = append(view.HistorySnapshots, cloneFileRecord(file))
		default:
			view.FilesMissingKinds = append(view.FilesMissingKinds, cloneFileRecord(file))
		}
	}
	return view, nil
}

func matchesFileHistoryOptions(file FileRecord, allowedKinds map[FileRecordKind]struct{}, includeUnknown bool) bool {
	if len(allowedKinds) == 0 && !includeUnknown {
		return true
	}
	if _, ok := allowedKinds[file.Kind]; ok {
		return true
	}
	if includeUnknown {
		switch file.Kind {
		case FileRecordWorkspace, FileRecordFetched, FileRecordHistory:
			return false
		default:
			return true
		}
	}
	return false
}
