package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"crawler-ai/internal/config"
	"crawler-ai/internal/oauth"
)

type StatusError struct {
	Operation  string
	StatusCode int
	Message    string
	Body       string
}

func (e *StatusError) Error() string {
	if e == nil {
		return "provider status error"
	}
	message := strings.TrimSpace(e.Message)
	if message == "" {
		message = "provider returned non-success status"
	}
	if e.StatusCode > 0 {
		message = fmt.Sprintf("%s (status %d)", message, e.StatusCode)
	}
	if strings.TrimSpace(e.Body) != "" {
		message += ": " + strings.TrimSpace(e.Body)
	}
	return message
}

func newStatusError(operation string, statusCode int, message string) error {
	return &StatusError{Operation: operation, StatusCode: statusCode, Message: message}
}

func newStatusErrorFromResponse(operation string, response *http.Response, fallback string) error {
	statusCode := 0
	body := ""
	if response != nil {
		statusCode = response.StatusCode
		body = readStatusErrorBody(response.Body)
	}
	message := strings.TrimSpace(fallback)
	if extracted := extractStatusErrorMessage(body); extracted != "" {
		message = extracted
	}
	return &StatusError{
		Operation:  operation,
		StatusCode: statusCode,
		Message:    message,
		Body:       body,
	}
}

func readStatusErrorBody(body io.Reader) string {
	if body == nil {
		return ""
	}
	data, err := io.ReadAll(io.LimitReader(body, 4096))
	if err != nil {
		return ""
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return ""
	}
	if json.Valid(data) {
		var compact bytes.Buffer
		if err := json.Compact(&compact, data); err == nil {
			trimmed = compact.String()
		}
	}
	if len(trimmed) > 512 {
		trimmed = trimmed[:509] + "..."
	}
	return trimmed
}

func extractStatusErrorMessage(body string) string {
	if strings.TrimSpace(body) == "" {
		return ""
	}
	var payload struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return ""
	}
	message := strings.TrimSpace(payload.Error.Message)
	if message == "" {
		return ""
	}
	errorType := strings.TrimSpace(payload.Error.Type)
	if errorType == "" {
		return message
	}
	return errorType + ": " + message
}

func isUnauthorized(err error) bool {
	statusErr, ok := err.(*StatusError)
	return ok && statusErr.StatusCode == http.StatusUnauthorized
}

type authProvider struct {
	mu         sync.Mutex
	role       Role
	baseConfig config.ProviderConfig
	factory    Factory
	store      *config.Store
	keyStore   *oauth.KeyStore
	provider   Provider
}

func newAuthProvider(role Role, cfg config.ProviderConfig, factory Factory, cfgStore *config.Store, keyStore *oauth.KeyStore) (Provider, error) {
	wrapped := &authProvider{
		role:       role,
		baseConfig: cfg,
		factory:    factory,
		store:      cfgStore,
		keyStore:   keyStore,
	}
	if err := wrapped.rebuild(); err != nil {
		return nil, err
	}
	return wrapped, nil
}

func (p *authProvider) Name() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.provider.Name()
}

func (p *authProvider) Complete(ctx context.Context, request Request) (Response, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.refreshExpiredOAuth(ctx); err != nil {
		return Response{}, err
	}

	response, err := p.provider.Complete(ctx, request)
	if err == nil || !isUnauthorized(err) {
		return response, err
	}

	if retryErr := p.refreshAfterUnauthorized(ctx); retryErr != nil {
		return response, err
	}
	return p.provider.Complete(ctx, request)
}

func (p *authProvider) CompleteStream(ctx context.Context, request Request, onChunk func(StreamChunk)) (Response, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	streaming, ok := SupportsStreaming(p.provider)
	if !ok {
		return p.completeWithoutStreaming(ctx, request, onChunk)
	}

	if err := p.refreshExpiredOAuth(ctx); err != nil {
		return Response{}, err
	}

	response, err := streaming.CompleteStream(ctx, request, onChunk)
	if err == nil || !isUnauthorized(err) {
		return response, err
	}

	if retryErr := p.refreshAfterUnauthorized(ctx); retryErr != nil {
		return response, err
	}

	streaming, ok = SupportsStreaming(p.provider)
	if !ok {
		return p.completeWithoutStreaming(ctx, request, onChunk)
	}
	return streaming.CompleteStream(ctx, request, onChunk)
}

func (p *authProvider) completeWithoutStreaming(ctx context.Context, request Request, onChunk func(StreamChunk)) (Response, error) {
	if err := p.refreshExpiredOAuth(ctx); err != nil {
		return Response{}, err
	}

	response, err := p.provider.Complete(ctx, request)
	if err != nil && isUnauthorized(err) {
		if retryErr := p.refreshAfterUnauthorized(ctx); retryErr == nil {
			response, err = p.provider.Complete(ctx, request)
		}
	}
	if err != nil {
		return Response{}, err
	}
	if onChunk != nil {
		for _, block := range response.ContentBlocks() {
			chunk := StreamChunk{Content: []ContentBlock{block}}
			switch block.Kind {
			case ContentBlockText:
				chunk.Text = block.Text
			case ContentBlockReasoning:
				chunk.Reasoning = block.Text
			}
			onChunk(chunk)
		}
		onChunk(StreamChunk{Done: true, Usage: response.Usage, FinishReason: response.FinishReason})
	}
	return response, nil
}

func (p *authProvider) refreshExpiredOAuth(ctx context.Context) error {
	if p.baseConfig.OAuthToken == nil || !p.baseConfig.OAuthToken.IsExpired() {
		return nil
	}
	return p.refreshOAuth(ctx)
}

func (p *authProvider) refreshAfterUnauthorized(ctx context.Context) error {
	if p.baseConfig.OAuthToken != nil {
		return p.refreshOAuth(ctx)
	}
	return p.reloadCredentials()
}

func (p *authProvider) refreshOAuth(ctx context.Context) error {
	if p.store != nil {
		scope, err := p.store.ScopeForRole(string(p.role))
		if err != nil {
			return err
		}
		if err := p.store.RefreshProviderOAuthToken(ctx, scope, string(p.role)); err != nil {
			return err
		}
		refreshedCfg, err := p.store.ProviderConfigForRole(string(p.role))
		if err != nil {
			return err
		}
		p.baseConfig = refreshedCfg
		return p.rebuild()
	}

	refreshed, err := oauth.RefreshToken(ctx, p.baseConfig.Provider, p.baseConfig.OAuthToken)
	if err != nil {
		return err
	}
	p.baseConfig.OAuthToken = refreshed
	p.baseConfig.APIKey = refreshed.AccessToken
	return p.rebuild()
}

func (p *authProvider) reloadCredentials() error {
	if p.keyStore != nil {
		_ = p.keyStore.Load()
	}
	return p.rebuild()
}

func (p *authProvider) rebuild() error {
	runtimeCfg, err := prepareRuntimeConfig(p.keyStore, p.baseConfig)
	if err != nil {
		return err
	}
	p.baseConfig = runtimeCfg
	p.provider = p.factory(runtimeCfg)
	return nil
}
