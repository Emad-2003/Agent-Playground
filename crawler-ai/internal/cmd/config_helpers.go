package cmd

import (
	"path/filepath"

	"crawler-ai/internal/config"
)

func loadCommandConfig() (config.Config, error) {
	if cwd == "" {
		return config.Load()
	}
	return config.LoadForWorkingDir(filepath.Clean(cwd))
}

func openConfigStore() (*config.Store, error) {
	if cwd == "" {
		return config.OpenStore("")
	}
	return config.OpenStore(filepath.Clean(cwd))
}
