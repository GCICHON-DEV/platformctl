package templateengine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"
)

const MaxTemplateBytes = 1 << 20
const SourceLockPath = ".platformctl/template.lock.json"
const maxLockBytes = 2 << 20

type SourceLock struct {
	Format   int    `json:"format"`
	Source   string `json:"source"`
	SHA256   string `json:"sha256"`
	Manifest []byte `json:"manifest"`
}

func boundedLocal(path string, maximum int64) ([]byte, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !stat.Mode().IsRegular() {
		return nil, fmt.Errorf("input must be a regular file")
	}
	return boundedRead(f, maximum)
}
func boundedRead(r io.Reader, maximum int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maximum {
		return nil, fmt.Errorf("template byte limit exceeded (%d)", maximum)
	}
	return data, nil
}
func checkRemote(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
		return fmt.Errorf("template requires HTTPS URL without userinfo or fragment")
	}
	for key := range u.Query() {
		if regexp.MustCompile(`(?i)(password|secret|token|credential|api[_-]?key|access[_-]?key|private[_-]?key)`).MatchString(key) {
			return fmt.Errorf("template URL must not contain secret query parameters")
		}
	}
	return nil
}
func downloadSource(source SourceInfo) ([]byte, error) {
	if err := checkRemote(source.Resolved); err != nil {
		return nil, err
	}
	client := http.Client{Timeout: 20 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 3 {
			return fmt.Errorf("template redirect limit exceeded (3)")
		}
		return checkRemote(req.URL.String())
	}}
	response, err := client.Get(source.Resolved)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("template download returned HTTP %d; cached fallback disabled", response.StatusCode)
	}
	return boundedRead(response.Body, MaxTemplateBytes)
}
func sourceLockDir() error {
	dir := filepath.Dir(SourceLockPath)
	if err := os.Mkdir(dir, 0700); err != nil && !os.IsExist(err) {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("template lock directory must not be a symlink")
	}
	return nil
}
func readSourceLock() (SourceLock, error) {
	var lock SourceLock
	dir, err := os.Lstat(filepath.Dir(SourceLockPath))
	if err != nil || !dir.IsDir() || dir.Mode()&os.ModeSymlink != 0 {
		return lock, fmt.Errorf("template lock directory missing or unsafe")
	}
	info, err := os.Lstat(SourceLockPath)
	if err != nil {
		return lock, fmt.Errorf("template source is not locked; run platformctl template lock: %w", err)
	}
	if !info.Mode().IsRegular() {
		return lock, fmt.Errorf("template lock must be a regular file")
	}
	data, err := boundedLocal(SourceLockPath, maxLockBytes)
	if err != nil {
		return lock, err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err = d.Decode(&lock); err != nil {
		return lock, err
	}
	var extra interface{}
	if d.Decode(&extra) != io.EOF {
		return lock, fmt.Errorf("template lock must contain one JSON object")
	}
	if lock.Format != 1 || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(lock.SHA256) || len(lock.Manifest) > MaxTemplateBytes || checksum(lock.Manifest) != lock.SHA256 {
		return lock, fmt.Errorf("template lock/cache is corrupt")
	}
	if err = checkRemote(lock.Source); err != nil {
		return lock, err
	}
	return lock, nil
}
func readTrustedSource(source SourceInfo, offline bool) ([]byte, error) {
	lock, err := readSourceLock()
	if err != nil {
		return nil, err
	}
	if lock.Source != source.Resolved {
		return nil, fmt.Errorf("template source differs from lock; explicitly run template update")
	}
	if offline {
		return lock.Manifest, nil
	}
	data, err := downloadSource(source)
	if err != nil {
		return nil, err
	}
	if checksum(data) != lock.SHA256 {
		return nil, fmt.Errorf("template digest differs from trusted lock; review source and explicitly run template update")
	}
	return data, nil
}

// TrustSource is an explicit trust/update operation, never part of Load or apply.
// The workspace lock held by the caller serializes it with plan/apply/destroy.
func TrustSource(template TemplateSource, update bool) (SourceLock, error) {
	var lock SourceLock
	source, err := ResolveTemplateSource(template)
	if err != nil {
		return lock, err
	}
	if source.Kind == "local" {
		return lock, fmt.Errorf("local templates use explicit local trust and preview snapshots; remote source required")
	}
	if err = sourceLockDir(); err != nil {
		return lock, err
	}
	_, err = os.Lstat(SourceLockPath)
	if update {
		if err != nil {
			return lock, fmt.Errorf("template update requires an existing lock: %w", err)
		}
		info, inspectErr := os.Lstat(SourceLockPath)
		if inspectErr != nil || !info.Mode().IsRegular() {
			return lock, fmt.Errorf("existing template lock must be a regular file")
		}
	} else if !os.IsNotExist(err) {
		return lock, fmt.Errorf("template lock exists or cannot be inspected; use template update explicitly")
	}
	data, err := downloadSource(source)
	if err != nil {
		return lock, err
	}
	var manifest Manifest
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err = decoder.Decode(&manifest); err != nil {
		return lock, err
	}
	normalizeManifest(&manifest)
	if manifest.APIVersion != "platformctl.io/v1beta1" || manifest.Name == "" {
		return lock, fmt.Errorf("source is not a v1beta1 manifest")
	}
	var extra interface{}
	if decoder.Decode(&extra) != io.EOF {
		return lock, fmt.Errorf("source must contain exactly one YAML document")
	}
	lock = SourceLock{Format: 1, Source: source.Resolved, SHA256: checksum(data), Manifest: data}
	encoded, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return lock, err
	}
	file, err := os.CreateTemp(filepath.Dir(SourceLockPath), ".template-lock-")
	if err != nil {
		return lock, err
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err = file.Write(append(encoded, '\n')); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return lock, err
	}
	if closeErr != nil {
		return lock, closeErr
	}
	if update {
		err = os.Rename(name, SourceLockPath)
	} else {
		err = os.Link(name, SourceLockPath)
	}
	if err != nil {
		return lock, err
	}
	return lock, nil
}

// ReadTemplateReference reads only bounded configuration without loading a source.
func ReadTemplateReference(path string) (TemplateSource, error) {
	var config PlatformFile
	data, err := boundedLocal(path, MaxTemplateBytes)
	if err != nil {
		return config.Template, err
	}
	d := yaml.NewDecoder(bytes.NewReader(data))
	d.KnownFields(true)
	if err = d.Decode(&config); err != nil {
		return config.Template, err
	}
	var extra interface{}
	if d.Decode(&extra) != io.EOF {
		return config.Template, fmt.Errorf("platform.yaml must contain one document")
	}
	return config.Template, nil
}
