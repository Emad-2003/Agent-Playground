package oauth

import (
	"context"
	"testing"
	"time"

	apperrors "crawler-ai/internal/errors"
)

func TestRefreshTokenUsesRegisteredRefresher(t *testing.T) {
	t.Parallel()

	unregister := RegisterRefresher("test", func(ctx context.Context, token *Token) (*Token, error) {
		return &Token{AccessToken: "new", RefreshToken: token.RefreshToken, ExpiresIn: 60}, nil
	})
	defer unregister()

	refreshed, err := RefreshToken(context.Background(), "test", &Token{AccessToken: "old", RefreshToken: "refresh"})
	if err != nil {
		t.Fatalf("RefreshToken() error: %v", err)
	}
	if refreshed.AccessToken != "new" {
		t.Fatalf("expected refreshed access token, got %s", refreshed.AccessToken)
	}
	if refreshed.ExpiresAt <= time.Now().Unix() {
		t.Fatal("expected expires_at to be derived from expires_in")
	}
}

func TestRefreshTokenRejectsUnknownProvider(t *testing.T) {
	t.Parallel()

	_, err := RefreshToken(context.Background(), "unknown", &Token{AccessToken: "old"})
	if err == nil {
		t.Fatal("expected refresh error")
	}
	if !apperrors.IsCode(err, apperrors.CodeInvalidConfig) {
		t.Fatalf("expected invalid config error, got %v", err)
	}
}
