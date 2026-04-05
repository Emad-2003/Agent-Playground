package tools

import (
	"cmp"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
)

var fastIgnoreDirs = map[string]bool{
	".git":            true,
	".svn":            true,
	".hg":             true,
	".bzr":            true,
	".vscode":         true,
	".idea":           true,
	"node_modules":    true,
	"__pycache__":     true,
	".pytest_cache":   true,
	".cache":          true,
	".tmp":            true,
	".Trash":          true,
	".Spotlight-V100": true,
	".fseventsd":      true,
	".crush":          true,
	".crawler-ai":     true,
	"OrbStack":        true,
	".local":          true,
	".share":          true,
}

var commonIgnorePatterns = sync.OnceValue(func() []gitignore.Pattern {
	patterns := []string{
		"*.swp", "*.swo", "*~", ".DS_Store", "Thumbs.db",
		"target", "build", "dist", "out", "bin", "obj",
		"*.o", "*.so", "*.dylib", "*.dll", "*.exe",
		"*.log", "*.tmp", "*.temp", "*.pyc", "*.pyo",
		"vendor", "Cargo.lock", "package-lock.json", "yarn.lock", "pnpm-lock.yaml",
	}
	return parsePatterns(patterns, nil)
})

var gitGlobalIgnorePatterns = sync.OnceValue(func() []gitignore.Pattern {
	cfg, err := gitconfig.LoadConfig(gitconfig.GlobalScope)
	if err != nil {
		return nil
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	configPath := cmp.Or(os.Getenv("XDG_CONFIG_HOME"), filepath.Join(homeDir, ".config"))
	excludesFilePath := cmp.Or(cfg.Raw.Section("core").Options.Get("excludesfile"), filepath.Join(configPath, "git", "ignore"))
	excludesFilePath = expandHomePath(excludesFilePath, homeDir)
	bts, err := os.ReadFile(excludesFilePath)
	if err != nil {
		return nil
	}
	return parsePatterns(strings.Split(string(bts), "\n"), nil)
})

var appGlobalIgnorePatterns = sync.OnceValue(func() []gitignore.Pattern {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	configPath := cmp.Or(os.Getenv("XDG_CONFIG_HOME"), filepath.Join(homeDir, ".config"))
	var patterns []gitignore.Pattern
	for _, name := range []string{filepath.Join(configPath, "crawler-ai", "ignore"), filepath.Join(configPath, "crush", "ignore")} {
		bts, err := os.ReadFile(name)
		if err == nil {
			patterns = append(patterns, parsePatterns(strings.Split(string(bts), "\n"), nil)...)
		}
	}
	return patterns
})

type directoryIgnorer struct {
	rootPath         string
	mu               sync.RWMutex
	dirPatterns      map[string][]gitignore.Pattern
	combinedMatchers map[string]gitignore.Matcher
}

type ignoredFileInfo struct {
	Path    string
	ModTime time.Time
}

func NewDirectoryIgnorer(rootPath string) *directoryIgnorer {
	return &directoryIgnorer{
		rootPath:         rootPath,
		dirPatterns:      make(map[string][]gitignore.Pattern),
		combinedMatchers: make(map[string]gitignore.Matcher),
	}
}

func pathToComponents(value string) []string {
	value = filepath.ToSlash(value)
	if value == "" || value == "." {
		return nil
	}
	return strings.Split(value, "/")
}

func parsePatterns(lines []string, domain []string) []gitignore.Pattern {
	patterns := make([]gitignore.Pattern, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, gitignore.ParsePattern(line, domain))
	}
	return patterns
}

func (di *directoryIgnorer) getDirPatterns(dir string) []gitignore.Pattern {
	di.mu.RLock()
	if cached, ok := di.dirPatterns[dir]; ok {
		di.mu.RUnlock()
		return cached
	}
	di.mu.RUnlock()
	var allPatterns []gitignore.Pattern
	relPath, _ := filepath.Rel(di.rootPath, dir)
	var domain []string
	if relPath != "" && relPath != "." {
		domain = pathToComponents(relPath)
	}
	for _, ignoreFile := range []string{".gitignore", ".crushignore", ".crawler-aiignore"} {
		ignorePath := filepath.Join(dir, ignoreFile)
		if content, err := os.ReadFile(ignorePath); err == nil {
			allPatterns = append(allPatterns, parsePatterns(strings.Split(string(content), "\n"), domain)...)
		}
	}
	di.mu.Lock()
	defer di.mu.Unlock()
	if cached, ok := di.dirPatterns[dir]; ok {
		return cached
	}
	di.dirPatterns[dir] = allPatterns
	return allPatterns
}

func (di *directoryIgnorer) getCombinedMatcher(dir string) gitignore.Matcher {
	di.mu.RLock()
	if matcher, ok := di.combinedMatchers[dir]; ok {
		di.mu.RUnlock()
		return matcher
	}
	di.mu.RUnlock()
	allPatterns := make([]gitignore.Pattern, 0)
	allPatterns = append(allPatterns, commonIgnorePatterns()...)
	allPatterns = append(allPatterns, gitGlobalIgnorePatterns()...)
	allPatterns = append(allPatterns, appGlobalIgnorePatterns()...)

	relDir, _ := filepath.Rel(di.rootPath, dir)
	currentPath := di.rootPath
	allPatterns = append(allPatterns, di.getDirPatterns(currentPath)...)
	if relDir != "" && relDir != "." {
		for _, part := range pathToComponents(relDir) {
			currentPath = filepath.Join(currentPath, part)
			allPatterns = append(allPatterns, di.getDirPatterns(currentPath)...)
		}
	}
	matcher := gitignore.NewMatcher(allPatterns)
	di.mu.Lock()
	defer di.mu.Unlock()
	if cached, ok := di.combinedMatchers[dir]; ok {
		return cached
	}
	di.combinedMatchers[dir] = matcher
	return matcher
}

func (di *directoryIgnorer) ShouldSkip(path string, isDir bool) bool {
	base := filepath.Base(path)
	if isDir && fastIgnoreDirs[base] {
		return true
	}
	if path == di.rootPath {
		return false
	}
	relPath, err := filepath.Rel(di.rootPath, path)
	if err != nil {
		relPath = path
	}
	components := pathToComponents(relPath)
	if len(components) == 0 {
		return false
	}
	matcher := di.getCombinedMatcher(filepath.Dir(path))
	return matcher.Match(components, isDir)
}

func GlobGitignoreAware(pattern, rootPath string, limit int) ([]string, bool, error) {
	pattern = filepath.ToSlash(pattern)
	ignorer := NewDirectoryIgnorer(rootPath)
	found := make([]ignoredFileInfo, 0)
	err := filepath.WalkDir(rootPath, func(currentPath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		isDir := entry.IsDir()
		if ignorer.ShouldSkip(currentPath, isDir) {
			if isDir {
				return filepath.SkipDir
			}
			return nil
		}
		if isDir {
			return nil
		}
		relPath, err := filepath.Rel(rootPath, currentPath)
		if err != nil {
			return nil
		}
		relPath = filepath.ToSlash(relPath)
		matched, err := doublestar.Match(pattern, relPath)
		if err != nil || !matched {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		found = append(found, ignoredFileInfo{Path: relPath, ModTime: info.ModTime()})
		if limit > 0 && len(found) >= limit*2 {
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil && !errors.Is(err, filepath.SkipAll) {
		return nil, false, err
	}
	sort.Slice(found, func(i, j int) bool {
		return found[i].ModTime.After(found[j].ModTime)
	})
	truncated := false
	if limit > 0 && len(found) > limit {
		found = found[:limit]
		truncated = true
	}
	results := make([]string, 0, len(found))
	for _, item := range found {
		results = append(results, item.Path)
	}
	return results, truncated || errors.Is(err, filepath.SkipAll), nil
}

func expandHomePath(pathValue, homeDir string) string {
	if pathValue == "~" {
		return homeDir
	}
	if strings.HasPrefix(pathValue, "~/") || strings.HasPrefix(pathValue, "~\\") {
		return filepath.Join(homeDir, pathValue[2:])
	}
	return pathValue
}
