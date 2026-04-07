package browserterm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"fascinate/internal/controlplane"
	"fascinate/internal/database"
)

const (
	execStateRunning   = "RUNNING"
	execStateSucceeded = "SUCCEEDED"
	execStateFailed    = "FAILED"
	execStateTimedOut  = "TIMED_OUT"
	execStateCancelled = "CANCELLED"
	execStateError     = "ERROR"

	execFailureCommandExit = "command_exit"
	execFailureTimeout     = "timeout"
	execFailureCancelled   = "cancelled"
	execFailureTransport   = "transport"

	execOutputStoreLimit = 256 * 1024
)

type Exec struct {
	ID                      string  `json:"id"`
	UserEmail               string  `json:"user_email,omitempty"`
	MachineName             string  `json:"machine_name"`
	HostID                  string  `json:"host_id,omitempty"`
	CommandText             string  `json:"command_text"`
	CWD                     string  `json:"cwd,omitempty"`
	State                   string  `json:"state"`
	RequestedTimeoutSeconds int     `json:"requested_timeout_seconds,omitempty"`
	ExitCode                *int    `json:"exit_code,omitempty"`
	FailureClass            string  `json:"failure_class,omitempty"`
	StdoutText              string  `json:"stdout_text,omitempty"`
	StderrText              string  `json:"stderr_text,omitempty"`
	StdoutTruncated         bool    `json:"stdout_truncated,omitempty"`
	StderrTruncated         bool    `json:"stderr_truncated,omitempty"`
	StartedAt               *string `json:"started_at,omitempty"`
	CompletedAt             *string `json:"completed_at,omitempty"`
	CancelRequestedAt       *string `json:"cancel_requested_at,omitempty"`
	CreatedAt               string  `json:"created_at"`
	UpdatedAt               string  `json:"updated_at"`
}

type ExecRequest struct {
	CommandText string
	CWD         string
	Timeout     time.Duration
	ID          string
}

type ExecStreamEvent struct {
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
	Exec *Exec  `json:"exec,omitempty"`
}

type ExecDiagnostics struct {
	Active int    `json:"active"`
	Execs  []Exec `json:"execs"`
}

type execJob struct {
	id        string
	userEmail string

	mu          sync.Mutex
	subscribers map[int]chan ExecStreamEvent
	nextSubID   int
	final       *Exec
	cancel      context.CancelFunc
	done        chan struct{}
	cancelled   bool
}

type cappedStringBuffer struct {
	limit     int
	builder   strings.Builder
	truncated bool
}

func (m *Manager) CreateExec(ctx context.Context, userEmail, machineName string, req ExecRequest) (Exec, error) {
	if m.store == nil {
		return Exec{}, fmt.Errorf("exec store is not configured")
	}
	commandText := strings.TrimSpace(req.CommandText)
	if commandText == "" {
		return Exec{}, fmt.Errorf("command is required")
	}

	machine, err := m.machines.GetMachine(ctx, machineName, userEmail)
	if err != nil {
		return Exec{}, err
	}
	if !strings.EqualFold(machine.State, "RUNNING") {
		return Exec{}, fmt.Errorf("machine %q must be running before exec is available", machineName)
	}
	hostID := strings.TrimSpace(machine.HostID)
	if hostID == "" {
		hostID = m.localHostID
	}
	if hostID != m.localHostID {
		return Exec{}, fmt.Errorf("exec gateway for host %q is not available", hostID)
	}
	user, err := m.store.GetUserByEmail(ctx, userEmail)
	if err != nil {
		return Exec{}, err
	}

	timeoutSeconds := 0
	if req.Timeout > 0 {
		timeoutSeconds = int(req.Timeout / time.Second)
		if timeoutSeconds <= 0 {
			timeoutSeconds = 1
		}
	}

	record, err := m.store.CreateExec(ctx, database.CreateExecParams{
		ID:                      strings.TrimSpace(req.ID),
		UserID:                  user.ID,
		MachineID:               machine.ID,
		HostID:                  stringPointer(hostID),
		CommandText:             commandText,
		CWD:                     strings.TrimSpace(req.CWD),
		State:                   execStateRunning,
		RequestedTimeoutSeconds: timeoutSeconds,
	})
	if err != nil {
		return Exec{}, err
	}

	jobCtx, cancel := context.WithCancel(context.Background())
	if req.Timeout > 0 {
		jobCtx, cancel = context.WithTimeout(jobCtx, req.Timeout)
	}

	job := &execJob{
		id:          record.ID,
		userEmail:   strings.ToLower(strings.TrimSpace(userEmail)),
		subscribers: map[int]chan ExecStreamEvent{},
		cancel:      cancel,
		done:        make(chan struct{}),
	}
	m.execMu.Lock()
	m.execJobs[record.ID] = job
	m.execMu.Unlock()

	m.recordExecEventBestEffort(record, "exec.started", map[string]any{
		"exec_id":         record.ID,
		"machine_id":      record.MachineID,
		"machine_name":    record.MachineName,
		"host_id":         execRecordHostID(record),
		"command_text":    record.CommandText,
		"cwd":             record.CWD,
		"timeout_seconds": record.RequestedTimeoutSeconds,
	})

	go m.runExec(jobCtx, job, record, machine)
	return execFromRecord(record), nil
}

func (m *Manager) GetExec(ctx context.Context, userEmail, execID string) (Exec, error) {
	record, err := m.loadExec(ctx, execID, userEmail)
	if err != nil {
		return Exec{}, err
	}
	return execFromRecord(record), nil
}

func (m *Manager) ListExecs(ctx context.Context, userEmail string, limit int) ([]Exec, error) {
	if m.store == nil {
		return nil, fmt.Errorf("exec store is not configured")
	}
	records, err := m.store.ListExecs(ctx, userEmail, limit)
	if err != nil {
		return nil, err
	}
	out := make([]Exec, 0, len(records))
	for _, record := range records {
		out = append(out, execFromRecord(record))
	}
	return out, nil
}

func (m *Manager) ExecDiagnostics(ctx context.Context, userEmail string, limit int) (ExecDiagnostics, error) {
	execs, err := m.ListExecs(ctx, userEmail, limit)
	if err != nil {
		return ExecDiagnostics{}, err
	}
	m.execMu.Lock()
	active := len(m.execJobs)
	m.execMu.Unlock()
	return ExecDiagnostics{Active: active, Execs: execs}, nil
}

func (m *Manager) CancelExec(ctx context.Context, userEmail, execID string) error {
	record, err := m.loadExec(ctx, execID, userEmail)
	if err != nil {
		return err
	}
	if record.CompletedAt != nil {
		return nil
	}
	if err := m.store.MarkExecCancelRequested(ctx, execID); err != nil && !errors.Is(err, database.ErrNotFound) {
		return err
	}

	m.execMu.Lock()
	job := m.execJobs[strings.TrimSpace(execID)]
	m.execMu.Unlock()
	if job != nil {
		job.mu.Lock()
		job.cancelled = true
		cancel := job.cancel
		job.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	}

	updated, loadErr := m.store.GetExecByID(ctx, execID)
	if loadErr == nil {
		m.recordExecEventBestEffort(updated, "exec.cancel.requested", map[string]any{
			"exec_id":      updated.ID,
			"machine_id":   updated.MachineID,
			"machine_name": updated.MachineName,
			"host_id":      execRecordHostID(updated),
		})
	}
	return nil
}

func (m *Manager) StreamExec(w http.ResponseWriter, r *http.Request, userEmail, execID string) error {
	record, err := m.loadExec(r.Context(), execID, userEmail)
	if err != nil {
		return err
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("response does not support streaming")
	}

	job := m.lookupExecJob(record.ID)
	if job == nil || record.CompletedAt != nil {
		if err := writeExecSSEEvent(w, ExecStreamEvent{Type: "result", Exec: execPointer(execFromRecord(record))}); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}

	ch, unsubscribe := job.subscribe()
	defer unsubscribe()
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return nil
		case event, ok := <-ch:
			if !ok {
				return nil
			}
			if err := writeExecSSEEvent(w, event); err != nil {
				return err
			}
			flusher.Flush()
			if event.Type == "result" {
				return nil
			}
		}
	}
}

func (m *Manager) runExec(ctx context.Context, job *execJob, record database.ExecRecord, machine controlplane.Machine) {
	defer func() {
		m.execMu.Lock()
		delete(m.execJobs, record.ID)
		m.execMu.Unlock()
		close(job.done)
	}()

	stdoutBuffer := cappedStringBuffer{limit: execOutputStoreLimit}
	stderrBuffer := cappedStringBuffer{limit: execOutputStoreLimit}

	state := execStateSucceeded
	var exitCode *int
	var failureClass *string

	args, err := m.execSSHArgs(ctx, machine, record.CWD, record.CommandText)
	if err != nil {
		state, exitCode, failureClass = classifyExecError(job, ctx, err)
		m.finishExec(job, record, state, exitCode, failureClass, stdoutBuffer, stderrBuffer, err)
		return
	}

	cmd := exec.CommandContext(ctx, m.sshClientBinary, args...)
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		state, exitCode, failureClass = classifyExecError(job, ctx, err)
		m.finishExec(job, record, state, exitCode, failureClass, stdoutBuffer, stderrBuffer, err)
		return
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		state, exitCode, failureClass = classifyExecError(job, ctx, err)
		m.finishExec(job, record, state, exitCode, failureClass, stdoutBuffer, stderrBuffer, err)
		return
	}

	if err := cmd.Start(); err != nil {
		state, exitCode, failureClass = classifyExecError(job, ctx, err)
		m.finishExec(job, record, state, exitCode, failureClass, stdoutBuffer, stderrBuffer, err)
		return
	}

	readErrCh := make(chan error, 2)
	go m.captureExecStream(job, "stdout", stdoutPipe, &stdoutBuffer, readErrCh)
	go m.captureExecStream(job, "stderr", stderrPipe, &stderrBuffer, readErrCh)

	waitErr := cmd.Wait()
	readErrOne := <-readErrCh
	readErrTwo := <-readErrCh
	if waitErr == nil {
		switch {
		case readErrOne != nil:
			waitErr = readErrOne
		case readErrTwo != nil:
			waitErr = readErrTwo
		}
	}

	if waitErr != nil {
		state, exitCode, failureClass = classifyExecError(job, ctx, waitErr)
		if state == execStateError {
			stderrBuffer.WriteString(waitErr.Error() + "\n")
		}
	}

	m.finishExec(job, record, state, exitCode, failureClass, stdoutBuffer, stderrBuffer, nil)
}

func (m *Manager) finishExec(job *execJob, record database.ExecRecord, state string, exitCode *int, failureClass *string, stdoutBuffer, stderrBuffer cappedStringBuffer, startErr error) {
	if startErr != nil {
		stderrBuffer.WriteString(strings.TrimSpace(startErr.Error()) + "\n")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	finished, err := m.store.CompleteExec(ctx, database.FinishExecParams{
		ID:              record.ID,
		State:           state,
		ExitCode:        exitCode,
		FailureClass:    failureClass,
		StdoutText:      stdoutBuffer.String(),
		StderrText:      stderrBuffer.String(),
		StdoutTruncated: stdoutBuffer.Truncated(),
		StderrTruncated: stderrBuffer.Truncated(),
	})
	if err == nil {
		payload := map[string]any{
			"exec_id":       finished.ID,
			"machine_id":    finished.MachineID,
			"machine_name":  finished.MachineName,
			"host_id":       execRecordHostID(finished),
			"state":         finished.State,
			"failure_class": derefString(finished.FailureClass),
		}
		if finished.ExitCode != nil {
			payload["exit_code"] = *finished.ExitCode
		}
		m.recordExecEventBestEffort(finished, "exec.completed", payload)
		record = finished
	}

	result := execFromRecord(record)
	job.setFinal(result)
	job.broadcast(ExecStreamEvent{Type: "result", Exec: &result})
	job.closeSubscribers()
}

func (m *Manager) captureExecStream(job *execJob, kind string, reader io.Reader, buffer *cappedStringBuffer, errCh chan<- error) {
	data := make([]byte, 16*1024)
	for {
		n, err := reader.Read(data)
		if n > 0 {
			chunk := string(data[:n])
			buffer.WriteString(chunk)
			job.broadcast(ExecStreamEvent{Type: kind, Data: chunk})
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				errCh <- nil
				return
			}
			errCh <- err
			return
		}
	}
}

func (m *Manager) execSSHArgs(ctx context.Context, machine controlplane.Machine, cwd, commandText string) ([]string, error) {
	if machine.Runtime == nil {
		return nil, fmt.Errorf("machine %q is not available", machine.Name)
	}
	targetHost := strings.TrimSpace(machine.Runtime.SSHHost)
	targetPort := machine.Runtime.SSHPort
	if targetHost == "" || targetPort <= 0 {
		return nil, fmt.Errorf("machine %q does not have a reachable guest shell endpoint", machine.Name)
	}
	guestUser := guestUserForMachine(machine)
	if err := m.waitForGuestAccess(ctx, machine, targetHost, targetPort, guestUser); err != nil {
		return nil, err
	}
	command := execGuestCommand(normalizeGuestCwd(cwd, guestUser), commandText)
	args := append(m.guestSSHArgs(guestUser, targetHost, targetPort), guestSSHRemoteCommand("xterm-256color", command))
	return args, nil
}

func execGuestCommand(cwd, commandText string) string {
	script := []string{
		"set -eu",
		"if [ -f /etc/fascinate/env.sh ]; then . /etc/fascinate/env.sh; fi",
	}
	if strings.TrimSpace(cwd) != "" {
		script = append(script, "cd "+shellLiteral(strings.TrimSpace(cwd)))
	}
	script = append(script, "if command -v bash >/dev/null 2>&1; then")
	script = append(script, "  exec bash -c "+shellLiteral(strings.TrimSpace(commandText)))
	script = append(script, "fi")
	script = append(script, "exec sh -c "+shellLiteral(strings.TrimSpace(commandText)))
	return strings.Join(script, "\n")
}

func classifyExecError(job *execJob, ctx context.Context, err error) (string, *int, *string) {
	if err == nil {
		return execStateSucceeded, nil, nil
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) || strings.Contains(strings.ToLower(strings.TrimSpace(err.Error())), "deadline exceeded") {
		return execStateTimedOut, nil, stringPointer(execFailureTimeout)
	}
	if job != nil && job.isCancelled() {
		return execStateCancelled, nil, stringPointer(execFailureCancelled)
	}
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) || strings.Contains(strings.ToLower(strings.TrimSpace(err.Error())), "context canceled") {
		return execStateCancelled, nil, stringPointer(execFailureCancelled)
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code := exitErr.ExitCode()
		return execStateFailed, &code, stringPointer(execFailureCommandExit)
	}
	return execStateError, nil, stringPointer(execFailureTransport)
}

func (m *Manager) loadExec(ctx context.Context, execID, userEmail string) (database.ExecRecord, error) {
	if m.store == nil {
		return database.ExecRecord{}, fmt.Errorf("exec store is not configured")
	}
	record, err := m.store.GetExecByID(ctx, execID)
	if err != nil {
		return database.ExecRecord{}, err
	}
	if !strings.EqualFold(record.UserEmail, userEmail) {
		return database.ExecRecord{}, database.ErrNotFound
	}
	return record, nil
}

func (m *Manager) lookupExecJob(execID string) *execJob {
	m.execMu.Lock()
	defer m.execMu.Unlock()
	return m.execJobs[strings.TrimSpace(execID)]
}

func (m *Manager) recordExecEventBestEffort(record database.ExecRecord, kind string, payload map[string]any) {
	if m == nil || m.store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	encoded, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_, _ = m.store.CreateEvent(ctx, database.CreateEventParams{
		ID:          uuid.NewString(),
		ActorUserID: &record.UserID,
		MachineID:   &record.MachineID,
		Kind:        kind,
		PayloadJSON: string(encoded),
	})
}

func execFromRecord(record database.ExecRecord) Exec {
	out := Exec{
		ID:                      record.ID,
		UserEmail:               record.UserEmail,
		MachineName:             record.MachineName,
		HostID:                  execRecordHostID(record),
		CommandText:             record.CommandText,
		CWD:                     record.CWD,
		State:                   record.State,
		RequestedTimeoutSeconds: record.RequestedTimeoutSeconds,
		ExitCode:                record.ExitCode,
		StdoutText:              record.StdoutText,
		StderrText:              record.StderrText,
		StdoutTruncated:         record.StdoutTruncated,
		StderrTruncated:         record.StderrTruncated,
		StartedAt:               record.StartedAt,
		CompletedAt:             record.CompletedAt,
		CancelRequestedAt:       record.CancelRequestedAt,
		CreatedAt:               record.CreatedAt,
		UpdatedAt:               record.UpdatedAt,
	}
	if record.FailureClass != nil {
		out.FailureClass = strings.TrimSpace(*record.FailureClass)
	}
	return out
}

func execRecordHostID(record database.ExecRecord) string {
	if record.HostID == nil {
		return ""
	}
	return strings.TrimSpace(*record.HostID)
}

func execPointer(value Exec) *Exec {
	return &value
}

func writeExecSSEEvent(w http.ResponseWriter, event ExecStreamEvent) error {
	body, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\n", event.Type); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", body); err != nil {
		return err
	}
	return nil
}

func (j *execJob) subscribe() (<-chan ExecStreamEvent, func()) {
	j.mu.Lock()
	defer j.mu.Unlock()
	ch := make(chan ExecStreamEvent, 128)
	if j.final != nil {
		ch <- ExecStreamEvent{Type: "result", Exec: execPointer(*j.final)}
		close(ch)
		return ch, func() {}
	}
	id := j.nextSubID
	j.nextSubID++
	j.subscribers[id] = ch
	return ch, func() {
		j.mu.Lock()
		subscriber, ok := j.subscribers[id]
		if ok {
			delete(j.subscribers, id)
		}
		j.mu.Unlock()
		if ok {
			close(subscriber)
		}
	}
}

func (j *execJob) broadcast(event ExecStreamEvent) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, ch := range j.subscribers {
		ch <- event
	}
}

func (j *execJob) setFinal(exec Exec) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.final = execPointer(exec)
}

func (j *execJob) closeSubscribers() {
	j.mu.Lock()
	subscribers := j.subscribers
	j.subscribers = map[int]chan ExecStreamEvent{}
	j.mu.Unlock()
	for _, ch := range subscribers {
		close(ch)
	}
}

func (j *execJob) isCancelled() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.cancelled
}

func (b *cappedStringBuffer) WriteString(value string) {
	if b.limit <= 0 || value == "" {
		return
	}
	remaining := b.limit - b.builder.Len()
	if remaining <= 0 {
		b.truncated = true
		return
	}
	if len(value) > remaining {
		b.builder.WriteString(value[:remaining])
		b.truncated = true
		return
	}
	b.builder.WriteString(value)
}

func (b cappedStringBuffer) String() string {
	return b.builder.String()
}

func (b cappedStringBuffer) Truncated() bool {
	return b.truncated
}
