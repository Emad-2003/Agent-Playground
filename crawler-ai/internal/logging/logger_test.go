package logging

import (
	"bytes"
	"strings"
	"testing"
)

func TestConfigureUsesJSONInProduction(t *testing.T) {
	buffer := bytes.NewBuffer(nil)
	_, err := Configure(Options{
		Environment: "production",
		Level:       "info",
		Writer:      buffer,
	})
	if err != nil {
		t.Fatalf("unexpected configure error: %v", err)
	}

	Info("hello", "foo", "bar")
	if !strings.Contains(buffer.String(), "\"msg\":\"hello\"") {
		t.Fatalf("expected JSON output, got %s", buffer.String())
	}
}

func TestConfigureRejectsInvalidLevel(t *testing.T) {
	_, err := Configure(Options{Environment: "development", Level: "trace"})
	if err == nil {
		t.Fatal("expected configure error")
	}
}
