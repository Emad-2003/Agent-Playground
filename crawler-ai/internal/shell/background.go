package shell

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

const MaxBackgroundJobs = 32

type syncCappedBuffer struct {
	mu  sync.RWMutex
	buf *cappedBuffer
}

func newSyncCappedBuffer(maxBytes int) *syncCappedBuffer {
	return &syncCappedBuffer{buf: newCappedBuffer(maxBytes)}
}

func (b *syncCappedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncCappedBuffer) String() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.buf.String()
}

type BackgroundJob struct {
	ID         string
	Command    string
	WorkingDir string
	stdout     *syncCappedBuffer
	stderr     *syncCappedBuffer
	done       chan struct{}
	ctx        context.Context
	cancel     context.CancelFunc
	exitErr    error
}

type BackgroundJobManager struct {
	mu      sync.RWMutex
	jobs    map[string]*BackgroundJob
	counter atomic.Uint64
}

var (
	backgroundManager     *BackgroundJobManager
	backgroundManagerOnce sync.Once
)

func GetBackgroundJobManager() *BackgroundJobManager {
	backgroundManagerOnce.Do(func() {
		backgroundManager = &BackgroundJobManager{jobs: make(map[string]*BackgroundJob)}
	})
	return backgroundManager
}

func (m *BackgroundJobManager) Start(ctx context.Context, workingDir, command string) (*BackgroundJob, error) {
	if err := CommandsBlocker()(command); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.jobs) >= MaxBackgroundJobs {
		return nil, fmt.Errorf("maximum number of background jobs (%d) reached", MaxBackgroundJobs)
	}
	id := fmt.Sprintf("job-%03d", m.counter.Add(1))
	jobCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	job := &BackgroundJob{
		ID:         id,
		Command:    command,
		WorkingDir: workingDir,
		stdout:     newSyncCappedBuffer(MaxOutputBytes),
		stderr:     newSyncCappedBuffer(MaxOutputBytes),
		done:       make(chan struct{}),
		ctx:        jobCtx,
		cancel:     cancel,
	}
	m.jobs[id] = job
	go func() {
		defer close(job.done)
		job.exitErr = execWithStreams(jobCtx, workingDir, command, job.stdout, job.stderr)
	}()
	return job, nil
}

func (m *BackgroundJobManager) Get(id string) (*BackgroundJob, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	job, ok := m.jobs[id]
	return job, ok
}

func (m *BackgroundJobManager) Kill(id string) error {
	m.mu.Lock()
	job, ok := m.jobs[id]
	if ok {
		delete(m.jobs, id)
	}
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("background job not found: %s", id)
	}
	job.cancel()
	<-job.done
	return nil
}

func (m *BackgroundJobManager) KillAll(ctx context.Context) {
	m.mu.Lock()
	jobs := make([]*BackgroundJob, 0, len(m.jobs))
	for _, job := range m.jobs {
		jobs = append(jobs, job)
	}
	m.jobs = make(map[string]*BackgroundJob)
	m.mu.Unlock()
	for _, job := range jobs {
		job.cancel()
		select {
		case <-job.done:
		case <-ctx.Done():
			return
		}
	}
}

func (j *BackgroundJob) GetOutput() (stdout, stderr string, done bool, err error) {
	select {
	case <-j.done:
		return j.stdout.String(), j.stderr.String(), true, j.exitErr
	default:
		return j.stdout.String(), j.stderr.String(), false, nil
	}
}

func (j *BackgroundJob) WaitContext(ctx context.Context) bool {
	select {
	case <-j.done:
		return true
	case <-ctx.Done():
		return false
	}
}

func (j *BackgroundJob) IsDone() bool {
	select {
	case <-j.done:
		return true
	default:
		return false
	}
}

func (j *BackgroundJob) Wait() {
	<-j.done
}

func killAllBackgroundJobs(timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	GetBackgroundJobManager().KillAll(ctx)
}
