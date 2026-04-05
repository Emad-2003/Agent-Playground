# crawler-ai

`crwlr` is the CLI entrypoint for this repository.

## Install

Use Go's install flow from the repo root:

```powershell
go install ./cmd/crwlr
```

After install, verify it is on your `PATH`:

```powershell
crwlr --help
```

If the command is not found on Windows, add `%USERPROFILE%\go\bin` or `%GOBIN%` to `PATH`.

## Tests

Run the test suite with:

```powershell
go test ./...
```

Unit tests intentionally stay next to their packages under `internal/...` because many of them use package-local APIs. A single top-level `tests/` folder is appropriate only for black-box integration coverage.
