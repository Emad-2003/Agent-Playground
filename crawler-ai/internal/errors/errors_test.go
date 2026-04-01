package apperrors

import (
	stderrors "errors"
	"testing"
)

func TestIsCodeMatchesWrappedError(t *testing.T) {
	t.Parallel()

	err := Wrap("test", CodeStartupFailed, stderrors.New("boom"), "startup failed")
	if !IsCode(err, CodeStartupFailed) {
		t.Fatal("expected startup failed code match")
	}
}

func TestIsCodeRejectsUnrelatedError(t *testing.T) {
	t.Parallel()

	if IsCode(stderrors.New("plain"), CodeInvalidConfig) {
		t.Fatal("expected plain error to reject code match")
	}
}
