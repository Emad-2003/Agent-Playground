package session

import (
	"context"
	"testing"
	"time"
)

func TestSupportedDiagnosticsFilesFiltersAndDeduplicatesSupportedExtensions(t *testing.T) {
	files := supportedDiagnosticsFiles([]string{"main.go", "web/index.html", "main.go", "pkg\\logic.go", "style.css", "app.js", "app.js", "README.md"})
	if len(files.Go) != 2 || files.Go[0] != "main.go" || files.Go[1] != "pkg/logic.go" {
		t.Fatalf("unexpected go diagnostics files: %#v", files.Go)
	}
	if len(files.HTML) != 1 || files.HTML[0] != "web/index.html" {
		t.Fatalf("unexpected html diagnostics files: %#v", files.HTML)
	}
	if len(files.CSS) != 1 || files.CSS[0] != "style.css" {
		t.Fatalf("unexpected css diagnostics files: %#v", files.CSS)
	}
	if len(files.JavaScript) != 1 || files.JavaScript[0] != "app.js" {
		t.Fatalf("unexpected javascript diagnostics files: %#v", files.JavaScript)
	}
}

func TestParseGoplsDiagnosticsHandlesRelativeAndAbsolutePaths(t *testing.T) {
	workspace := `C:\repo`
	output := []byte("main.go:12:3-7: undefined: playMove\nC:\\repo\\pkg\\game.go:24: missing return\n")
	diagnostics := parseGoplsDiagnostics(output, "fallback.go", workspace)
	if len(diagnostics) != 2 {
		t.Fatalf("expected 2 diagnostics, got %#v", diagnostics)
	}
	if diagnostics[0].Path != "main.go" || diagnostics[0].Line != 12 || diagnostics[0].Column != 3 || diagnostics[0].Severity != "error" {
		t.Fatalf("unexpected first diagnostic: %#v", diagnostics[0])
	}
	if diagnostics[1].Path != "pkg/game.go" || diagnostics[1].Line != 24 {
		t.Fatalf("unexpected second diagnostic: %#v", diagnostics[1])
	}
}

func TestParseNodeCheckDiagnosticsHandlesSyntaxError(t *testing.T) {
	workspace := `C:\repo`
	output := []byte("C:\\repo\\web\\app.js:7\nfunction () {\n^^^^^^^^\n\nSyntaxError: Function statements require a function name\n")
	diagnostics := parseNodeCheckDiagnostics(output, "web/app.js", workspace)
	if len(diagnostics) != 1 {
		t.Fatalf("expected one diagnostic, got %#v", diagnostics)
	}
	if diagnostics[0].Path != "web/app.js" || diagnostics[0].Line != 7 || diagnostics[0].Column != 1 {
		t.Fatalf("unexpected node diagnostic payload: %#v", diagnostics[0])
	}
	if diagnostics[0].Message != "Function statements require a function name" {
		t.Fatalf("unexpected node diagnostic message: %#v", diagnostics[0])
	}
}

func TestParseHTMLHintDiagnosticsHandlesJSONPayload(t *testing.T) {
	workspace := `C:\repo`
	output := []byte(`[{"file":"C:\\repo\\index.html","messages":[{"type":"error","message":"Doctype must be declared before any non-comment content.","line":1,"col":1,"rule":{"id":"doctype-first"}}]}]`)
	diagnostics := parseHTMLHintDiagnostics(output, "index.html", workspace)
	if len(diagnostics) != 1 {
		t.Fatalf("expected one html diagnostic, got %#v", diagnostics)
	}
	if diagnostics[0].Path != "index.html" || diagnostics[0].Line != 1 || diagnostics[0].Column != 1 || diagnostics[0].Source != "htmlhint" {
		t.Fatalf("unexpected html diagnostic payload: %#v", diagnostics[0])
	}
}

func TestParseStylelintDiagnosticsHandlesJSONPayload(t *testing.T) {
	workspace := `C:\repo`
	output := []byte(`[{"source":"C:\\repo\\style.css","parseErrors":[],"warnings":[{"line":1,"column":15,"severity":"error","text":"Unexpected invalid hex color \"#12\" (color-no-invalid-hex)"}]}]`)
	diagnostics := parseStylelintDiagnostics(output, "style.css", workspace)
	if len(diagnostics) != 1 {
		t.Fatalf("expected one css diagnostic, got %#v", diagnostics)
	}
	if diagnostics[0].Path != "style.css" || diagnostics[0].Line != 1 || diagnostics[0].Column != 15 || diagnostics[0].Source != "stylelint" {
		t.Fatalf("unexpected css diagnostic payload: %#v", diagnostics[0])
	}
}

func TestCommandWorkspaceDiagnosticsProviderParsesAllCommandOutputs(t *testing.T) {
	provider := &CommandWorkspaceDiagnosticsProvider{
		timeout: time.Second,
		lookPath: func(name string) (string, error) {
			return name, nil
		},
		run: func(ctx context.Context, name string, args []string, dir string) ([]byte, error) {
			if dir != "/workspace" {
				t.Fatalf("unexpected workspace dir: %s", dir)
			}
			switch name {
			case "gopls":
				return []byte("main.go:8:2-6: undefined: mainn"), context.Canceled
			case "node":
				return []byte("app.js:3\nfunction () {\n^^^^^^^^\n\nSyntaxError: Function statements require a function name\n"), context.Canceled
			case "npx":
				if len(args) > 1 && args[1] == "htmlhint" {
					return []byte(`[{"file":"/workspace/index.html","messages":[{"type":"error","message":"Doctype must be declared before any non-comment content.","line":1,"col":1,"rule":{"id":"doctype-first"}}]}]`), context.Canceled
				}
				return []byte(`[{"source":"/workspace/style.css","parseErrors":[],"warnings":[{"line":1,"column":15,"severity":"error","text":"Unexpected invalid hex color \"#12\" (color-no-invalid-hex)"}]}]`), context.Canceled
			default:
				t.Fatalf("unexpected command name: %s", name)
			}
			return nil, nil
		},
	}

	result, err := provider.Diagnostics("/workspace", []string{"main.go", "README.md", "app.js", "index.html", "style.css"})
	if err != nil {
		t.Fatalf("Diagnostics() error: %v", err)
	}
	if len(result.Diagnostics) != 4 {
		t.Fatalf("expected four diagnostics, got %#v", result)
	}
	if len(result.Notes) != 0 {
		t.Fatalf("expected no notes, got %#v", result.Notes)
	}
}

func TestCommandWorkspaceDiagnosticsProviderReportsMissingToolAsNote(t *testing.T) {
	provider := &CommandWorkspaceDiagnosticsProvider{
		timeout: time.Second,
		lookPath: func(name string) (string, error) {
			if name == "gopls" {
				return "", context.DeadlineExceeded
			}
			return name, nil
		},
		run: func(context.Context, string, []string, string) ([]byte, error) {
			t.Fatal("run should not be called when gopls is missing")
			return nil, nil
		},
	}

	result, err := provider.Diagnostics("/workspace", []string{"main.go"})
	if err != nil {
		t.Fatalf("Diagnostics() error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("expected no diagnostics, got %#v", result.Diagnostics)
	}
	if len(result.Notes) != 1 || result.Notes[0] == "" {
		t.Fatalf("expected missing-tool note, got %#v", result.Notes)
	}
}
