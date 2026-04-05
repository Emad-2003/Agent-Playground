package oauth

import "time"

// Token represents an OAuth2 token persisted in config.
type Token struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresIn    int    `json:"expires_in,omitempty"`
	ExpiresAt    int64  `json:"expires_at,omitempty"`
}

func (t *Token) Clone() *Token {
	if t == nil {
		return nil
	}
	cloned := *t
	return &cloned
}

// SetExpiresAt calculates and stores the expiry timestamp from ExpiresIn.
func (t *Token) SetExpiresAt() {
	if t == nil || t.ExpiresIn <= 0 {
		return
	}
	t.ExpiresAt = time.Now().Add(time.Duration(t.ExpiresIn) * time.Second).Unix()
}

// SetExpiresIn recalculates the remaining lifetime from ExpiresAt.
func (t *Token) SetExpiresIn() {
	if t == nil || t.ExpiresAt <= 0 {
		return
	}
	t.ExpiresIn = int(time.Until(time.Unix(t.ExpiresAt, 0)).Seconds())
}

// IsExpired reports whether the token is expired or inside a 10% refresh buffer.
func (t *Token) IsExpired() bool {
	if t == nil || t.ExpiresAt <= 0 || t.ExpiresIn <= 0 {
		return false
	}
	return time.Now().Unix() >= (t.ExpiresAt - int64(t.ExpiresIn)/10)
}
