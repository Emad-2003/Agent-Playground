package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	apperrors "crawler-ai/internal/errors"

	"golang.org/x/net/html"
)

const (
	defaultFetchTimeout   = 30 * time.Second
	maxFetchResponseBytes = 256 * 1024
	largeFetchThreshold   = 50000
)

func (e *Executor) Fetch(ctx context.Context, rawURL string) (Result, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed == nil || parsed.Scheme == "" || parsed.Host == "" {
		return Result{}, apperrors.New("tools.Fetch", apperrors.CodeInvalidArgument, "fetch URL must be a valid absolute http or https URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return Result{}, apperrors.New("tools.Fetch", apperrors.CodeInvalidArgument, "fetch URL must use http or https")
	}
	requestCtx, cancel := context.WithTimeout(ctx, defaultFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return Result{}, apperrors.Wrap("tools.Fetch", apperrors.CodeToolFailed, err, "create fetch request")
	}
	req.Header.Set("User-Agent", "crawler-ai/1.0")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return Result{}, apperrors.Wrap("tools.Fetch", apperrors.CodeToolFailed, err, "send fetch request")
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return Result{}, apperrors.New("tools.Fetch", apperrors.CodeToolFailed, fmt.Sprintf("fetch returned status %d", resp.StatusCode))
	}

	body, truncated, err := readLimitedBody(resp.Body, maxFetchResponseBytes)
	if err != nil {
		return Result{}, apperrors.Wrap("tools.Fetch", apperrors.CodeToolFailed, err, "read fetch response")
	}
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	normalized, err := normalizeFetchedBody(contentType, body)
	if err != nil {
		return Result{}, err
	}
	if truncated {
		normalized = strings.TrimSpace(normalized) + "\n\n[response truncated at 262144 bytes]"
	}
	if len(normalized) > largeFetchThreshold {
		savedPath, err := e.saveFetchedContent(normalized)
		if err != nil {
			return Result{}, err
		}
		return Result{
			Output: fmt.Sprintf("Fetched content from %s (large page)\n\nContent saved to: %s\n\nUse the view and grep tools to analyze this file.", parsed.String(), savedPath),
			Extra:  map[string]string{"url": parsed.String(), "saved_path": savedPath, "large_content": "true"},
		}, nil
	}
	return Result{Output: strings.TrimSpace(normalized), Extra: map[string]string{"url": parsed.String()}}, nil
}

func (e *Executor) saveFetchedContent(content string) (string, error) {
	fetchDir := filepath.Join(e.workspaceRoot, ".crawler-ai", "fetch")
	if err := os.MkdirAll(fetchDir, 0o755); err != nil {
		return "", apperrors.Wrap("tools.saveFetchedContent", apperrors.CodeToolFailed, err, "create fetch directory")
	}
	tempFile, err := os.CreateTemp(fetchDir, "page-*.md")
	if err != nil {
		return "", apperrors.Wrap("tools.saveFetchedContent", apperrors.CodeToolFailed, err, "create fetch output file")
	}
	defer tempFile.Close()
	if _, err := tempFile.WriteString(content); err != nil {
		return "", apperrors.Wrap("tools.saveFetchedContent", apperrors.CodeToolFailed, err, "write fetched content")
	}
	relative, err := filepath.Rel(e.workspaceRoot, tempFile.Name())
	if err != nil {
		return "", apperrors.Wrap("tools.saveFetchedContent", apperrors.CodeToolFailed, err, "resolve fetch output path")
	}
	return filepath.ToSlash(relative), nil
}

func readLimitedBody(body io.Reader, maxBytes int64) ([]byte, bool, error) {
	limited := io.LimitReader(body, maxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, false, err
	}
	if int64(len(data)) > maxBytes {
		return data[:maxBytes], true, nil
	}
	return data, false, nil
}

func normalizeFetchedBody(contentType string, body []byte) (string, error) {
	trimmedType := strings.TrimSpace(strings.Split(contentType, ";")[0])
	switch {
	case strings.Contains(trimmedType, "text/html") || trimmedType == "":
		return extractHTMLText(string(body))
	case strings.HasPrefix(trimmedType, "text/"), strings.Contains(trimmedType, "json"), strings.Contains(trimmedType, "xml"):
		return string(body), nil
	default:
		return "", apperrors.New("tools.normalizeFetchedBody", apperrors.CodeToolFailed, "unsupported response content type for fetch")
	}
}

func extractHTMLText(source string) (string, error) {
	root, err := html.Parse(strings.NewReader(source))
	if err != nil {
		return "", apperrors.Wrap("tools.extractHTMLText", apperrors.CodeToolFailed, err, "parse html")
	}
	var parts []string
	var walk func(*html.Node, bool)
	walk = func(node *html.Node, skip bool) {
		if node == nil {
			return
		}
		if node.Type == html.ElementNode && (node.Data == "script" || node.Data == "style") {
			skip = true
		}
		if !skip && node.Type == html.TextNode {
			text := strings.Join(strings.Fields(node.Data), " ")
			if text != "" {
				parts = append(parts, text)
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child, skip)
		}
	}
	walk(root, false)
	return strings.Join(parts, "\n"), nil
}
