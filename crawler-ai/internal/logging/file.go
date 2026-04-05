package logging

import (
	"os"
	"path/filepath"
	"runtime"

	"crawler-ai/internal/oauth"
)

// SetupFileLogging configures an additional JSON log file at the platform-specific location.
// Returns the log file path and a cleanup function, or error.
func SetupFileLogging(level string) (string, func(), error) {
	logPath := DefaultLogPath()
	dir := filepath.Dir(logPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", nil, err
	}

	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return "", nil, err
	}

	_, err = Configure(Options{
		Environment: "production", // JSON format for file
		Level:       level,
		Writer:      file,
	})
	if err != nil {
		file.Close()
		return "", nil, err
	}

	cleanup := func() {
		file.Close()
	}

	return logPath, cleanup, nil
}

// DefaultLogPath returns the platform-specific log file path.
func DefaultLogPath() string {
	if configDir := oauth.ConfigDirOverride(); configDir != "" {
		return filepath.Join(configDir, "logs", "crawler-ai.log")
	}

	if runtime.GOOS == "windows" {
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			return filepath.Join(localAppData, "crawler-ai", "logs", "crawler-ai.log")
		}
	}
	home, _ := os.UserHomeDir()
	if home == "" {
		return "crawler-ai.log"
	}
	stateDir := os.Getenv("XDG_STATE_HOME")
	if stateDir == "" {
		stateDir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(stateDir, "crawler-ai", "logs", "crawler-ai.log")
}
