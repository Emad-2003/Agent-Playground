package oauth

import (
	"context"
	"strings"
	"sync"

	apperrors "crawler-ai/internal/errors"
)

type RefreshFunc func(ctx context.Context, token *Token) (*Token, error)

var (
	refreshersMu sync.RWMutex
	refreshers   = map[string]RefreshFunc{}
)

func RegisterRefresher(provider string, refreshFunc RefreshFunc) func() {
	provider = normalizeProvider(provider)
	refreshersMu.Lock()
	refreshers[provider] = refreshFunc
	refreshersMu.Unlock()
	return func() {
		refreshersMu.Lock()
		delete(refreshers, provider)
		refreshersMu.Unlock()
	}
}

func RefreshToken(ctx context.Context, provider string, token *Token) (*Token, error) {
	if token == nil {
		return nil, apperrors.New("oauth.RefreshToken", apperrors.CodeInvalidArgument, "oauth token must not be nil")
	}

	provider = normalizeProvider(provider)
	refreshersMu.RLock()
	refreshFunc, ok := refreshers[provider]
	refreshersMu.RUnlock()
	if !ok {
		return nil, apperrors.New("oauth.RefreshToken", apperrors.CodeInvalidConfig, "oauth refresh is not registered for provider "+provider)
	}

	refreshed, err := refreshFunc(ctx, token.Clone())
	if err != nil {
		return nil, err
	}
	if refreshed != nil {
		if refreshed.ExpiresAt == 0 && refreshed.ExpiresIn > 0 {
			refreshed.SetExpiresAt()
		}
		if refreshed.ExpiresIn == 0 && refreshed.ExpiresAt > 0 {
			refreshed.SetExpiresIn()
		}
	}
	return refreshed, nil
}

func normalizeProvider(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}
