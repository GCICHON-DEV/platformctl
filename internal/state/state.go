package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

type Lock struct {
	path string
}

func AcquireLock(operation string) (*Lock, error) {
	if err := os.MkdirAll(DirName, 0755); err != nil {
		return nil, fmt.Errorf("create state directory: %w", err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		file, err := os.OpenFile(LockPath(), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if errors.Is(err, os.ErrExist) {
			data, readErr := os.ReadFile(LockPath())
			if readErr == nil {
				if pid, ok := parseLockPID(string(data)); ok && !processExists(pid) {
					if removeErr := os.Remove(LockPath()); removeErr == nil {
						continue
					}
				}
				return nil, fmt.Errorf("another platformctl workflow is running: %s", strings.TrimSpace(string(data)))
			}
			return nil, fmt.Errorf("another platformctl workflow is running")
		}
		if err != nil {
			return nil, fmt.Errorf("create workflow lock: %w", err)
		}
		content := fmt.Sprintf("operation=%s pid=%s started_at=%s\n", operation, strconv.Itoa(os.Getpid()), time.Now().UTC().Format(time.RFC3339))
		if _, err := file.WriteString(content); err != nil {
			_ = file.Close()
			_ = os.Remove(LockPath())
			return nil, fmt.Errorf("write workflow lock: %w", err)
		}
		if err := file.Close(); err != nil {
			_ = os.Remove(LockPath())
			return nil, fmt.Errorf("close workflow lock: %w", err)
		}
		return &Lock{path: LockPath()}, nil
	}
	return nil, fmt.Errorf("another platformctl workflow is running")
}

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

func (l *Lock) Release() error {
	if l == nil || l.path == "" {
		return nil
	}
	if err := os.Remove(l.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("release workflow lock: %w", err)
	}
	return nil
}
