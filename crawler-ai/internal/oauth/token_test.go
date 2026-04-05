package oauth

import (
	"testing"
	"time"
)

func TestTokenClone(t *testing.T) {
	t.Parallel()

	token := &Token{AccessToken: "a", RefreshToken: "r", ExpiresIn: 10, ExpiresAt: 20}
	cloned := token.Clone()
	if cloned == token {
		t.Fatal("expected clone to create a distinct pointer")
	}
	if cloned.AccessToken != token.AccessToken {
		t.Fatal("expected clone to preserve values")
	}
}

func TestTokenSetExpiresAtAndIsExpired(t *testing.T) {
	t.Parallel()

	token := &Token{ExpiresIn: 10}
	token.SetExpiresAt()
	if token.ExpiresAt == 0 {
		t.Fatal("expected expires_at to be set")
	}

	expired := &Token{ExpiresIn: 100, ExpiresAt: time.Now().Add(-time.Minute).Unix()}
	if !expired.IsExpired() {
		t.Fatal("expected expired token to report expired")
	}
}
