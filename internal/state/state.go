package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	DirName  = ".platformctl"
	FileName = "state.json"
	LockName = "lock"
)

type State struct {
	TemplateSource   string            `json:"template_source"`
	TemplateVersion  string            `json:"template_version,omitempty"`
	TemplateChecksum string            `json:"template_checksum,omitempty"`
	GeneratedHash    string            `json:"generated_hash,omitempty"`
	LastPlanHash     string            `json:"last_plan_hash,omitempty"`
	ManagedFiles     []string          `json:"managed_files,omitempty"`
	LastPhase        string            `json:"last_phase,omitempty"`
	CompletedSteps   map[string]bool   `json:"completed_steps,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
	UpdatedAt        time.Time         `json:"updated_at"`
}

func Path() string {
	return filepath.Join(DirName, FileName)
}

func LockPath() string {
	return filepath.Join(DirName, LockName)
}

func Load() (*State, error) {
	data, err := os.ReadFile(Path())
	if errors.Is(err, os.ErrNotExist) {
		return &State{CompletedSteps: map[string]bool{}, Metadata: map[string]string{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read state: %w", err)
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("parse state: %w", err)
	}
	if st.CompletedSteps == nil {
		st.CompletedSteps = map[string]bool{}
	}
	if st.Metadata == nil {
		st.Metadata = map[string]string{}
	}
	if st.ManagedFiles == nil {
		st.ManagedFiles = []string{}
	}
	return &st, nil
}

func Save(st *State) error {
	if st.CompletedSteps == nil {
		st.CompletedSteps = map[string]bool{}
	}
	if st.Metadata == nil {
		st.Metadata = map[string]string{}
	}
	if st.ManagedFiles == nil {
		st.ManagedFiles = []string{}
	}
	st.UpdatedAt = time.Now().UTC()
	if err := os.MkdirAll(DirName, 0755); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(DirName, FileName+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary state file: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("write temporary state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close temporary state: %w", err)
	}
	if err := os.Rename(tmpName, Path()); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("write state: %w", err)
	}
	return nil
}

func ResetPhase(st *State, phase string) {
	st.LastPhase = phase
	st.CompletedSteps = map[string]bool{}
}

// Lock holds an exclusive kernel lock on a stable inode. Never unlink its path:
// a contender may already have opened that inode. Metadata is diagnostic only.
type Lock struct {
	file *os.File
	once sync.Once
	err  error
}

const lockFormat = "platformctl-lock-v2\n"

func AcquireLock(operation string) (*Lock, error) {
	if err := os.MkdirAll(DirName, 0755); err != nil {
		return nil, fmt.Errorf("create state directory: %w", err)
	}
	// O_NOFOLLOW prevents a workspace lock symlink from redirecting writes.
	fd, err := syscall.Open(LockPath(), syscall.O_RDWR|syscall.O_CREAT|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0644)
	if err != nil {
		return nil, fmt.Errorf("open workflow lock: %w", err)
	}
	file := os.NewFile(uintptr(fd), LockPath())
	fail := func(err error) (*Lock, error) { _ = file.Close(); return nil, err }
	for {
		err = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
		if err != syscall.EINTR {
			break
		}
	}
	if err != nil {
		return fail(fmt.Errorf("another platformctl workflow is running (or locking is unavailable): %w", err))
	}
	info, err := file.Stat()
	if err != nil {
		return fail(fmt.Errorf("inspect workflow lock: %w", err))
	}
	current, err := os.Lstat(LockPath())
	if err != nil || !info.Mode().IsRegular() || !os.SameFile(info, current) {
		return fail(fmt.Errorf("workflow lock path changed or is not a regular file"))
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return fail(fmt.Errorf("read workflow lock: %w", err))
	}
	// Older binaries use an existence/PID lock. Do not take over a live or
	// unrecognizable legacy lock. PID reuse in this format fails closed.
	if len(data) > 0 && !strings.HasPrefix(string(data), lockFormat) {
		pid, ok := parseLockPID(string(data))
		if !ok || processExists(pid) {
			return fail(fmt.Errorf("legacy workflow lock requires an idle workspace: %s", strings.TrimSpace(string(data))))
		}
	}
	content := fmt.Sprintf("%soperation=%s pid=%d started_at=%s\n", lockFormat, operation, os.Getpid(), time.Now().UTC().Format(time.RFC3339Nano))
	if err := file.Truncate(0); err != nil {
		return fail(fmt.Errorf("truncate workflow lock: %w", err))
	}
	if _, err := file.WriteAt([]byte(content), 0); err != nil {
		return fail(fmt.Errorf("write workflow lock: %w", err))
	}
	return &Lock{file: file}, nil
}

// File is borrowed by the synchronous step runner through exec.Cmd.ExtraFiles.
// A surviving step retains the lock after a CLI crash until it closes its copy.
func (l *Lock) File() *os.File { return l.file }

func parseLockPID(content string) (int, bool) {
	for _, token := range strings.Fields(content) {
		if !strings.HasPrefix(token, "pid=") {
			continue
		}
		pidText := strings.TrimPrefix(token, "pid=")
		pid, err := strconv.Atoi(pidText)
		if err != nil || pid <= 0 {
			return 0, false
		}
		return pid, true
	}
	return 0, false
}

func processExists(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	if errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH) {
		return false
	}
	return true
}

// Release closes only our descriptor. Explicit LOCK_UN would also unlock copies
// inherited by a still-running child. Repeated Release cannot affect a new owner.
func (l *Lock) Release() error {
	if l == nil {
		return nil
	}
	l.once.Do(func() {
		if err := l.file.Close(); err != nil {
			l.err = fmt.Errorf("release workflow lock: %w", err)
		}
	})
	return l.err
}
