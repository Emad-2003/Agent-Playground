package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

var getRg = sync.OnceValue(func() string {
	path, err := exec.LookPath("rg")
	if err != nil {
		return ""
	}
	return path
})

func getRgCmd(ctx context.Context, rootPath, globPattern string) *exec.Cmd {
	name := getRg()
	if name == "" {
		return nil
	}
	args := []string{"--files", "-L", "--null"}
	if globPattern != "" {
		if !filepath.IsAbs(globPattern) && !strings.HasPrefix(globPattern, "/") {
			globPattern = "/" + globPattern
		}
		args = append(args, "--glob", globPattern)
	}
	for _, ignoreFile := range []string{".crushignore", ".crawler-aiignore"} {
		ignorePath := filepath.Join(rootPath, ignoreFile)
		if _, err := os.Stat(ignorePath); err == nil {
			args = append(args, "--ignore-file", ignorePath)
		}
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = rootPath
	return cmd
}

func getRgSearchCmd(ctx context.Context, pattern, rootPath, include string) *exec.Cmd {
	name := getRg()
	if name == "" {
		return nil
	}
	args := []string{"--json", "-H", "-n", "-0", pattern}
	if include != "" {
		args = append(args, "--glob", include)
	}
	for _, ignoreFile := range []string{".gitignore", ".crushignore", ".crawler-aiignore"} {
		ignorePath := filepath.Join(rootPath, ignoreFile)
		if _, err := os.Stat(ignorePath); err == nil {
			args = append(args, "--ignore-file", ignorePath)
		}
	}
	args = append(args, rootPath)
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = rootPath
	return cmd
}
