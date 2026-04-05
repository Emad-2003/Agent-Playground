package shell

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestBackgroundJobManagerStartAndOutput(t *testing.T) {
	t.Parallel()
	manager := &BackgroundJobManager{jobs: make(map[string]*BackgroundJob)}
	job, err := manager.Start(context.Background(), t.TempDir(), "echo hello background")
	if err != nil {
		t.Fatalf("unexpected start error: %v", err)
	}
	job.Wait()
	stdout, stderr, done, err := job.GetOutput()
	if err != nil {
		t.Fatalf("unexpected output error: %v", err)
	}
	if !done || !strings.Contains(stdout, "hello background") || stderr != "" {
		t.Fatalf("unexpected background output stdout=%q stderr=%q done=%v", stdout, stderr, done)
	}
	if killErr := manager.Kill(job.ID); killErr != nil {
		t.Fatalf("unexpected kill error: %v", killErr)
	}
}

func TestBackgroundJobManagerKill(t *testing.T) {
	t.Parallel()
	manager := &BackgroundJobManager{jobs: make(map[string]*BackgroundJob)}
	command := "sleep 5"
	if runtime.GOOS == "windows" {
		command = "ping 127.0.0.1 -n 6 >NUL"
	}
	job, err := manager.Start(context.Background(), t.TempDir(), command)
	if err != nil {
		t.Fatalf("unexpected start error: %v", err)
	}
	if err := manager.Kill(job.ID); err != nil {
		t.Fatalf("unexpected kill error: %v", err)
	}
	if !job.WaitContext(context.Background()) {
		t.Fatal("expected killed job to be done")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	manager.KillAll(ctx)
}
