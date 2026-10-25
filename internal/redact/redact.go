// Package redact implements bounded diagnostic redaction, not a secret detector.
package redact

import (
	"encoding/json"
	"io"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
)

const Marker = "[REDACTED]"
const MaxLine = 64 << 10

var namePattern = regexp.MustCompile(`(?i)(password|passwd|secret|token|credential|api[_-]?key|access[_-]?key|private[_-]?key)`)
var assignments = regexp.MustCompile(`(?i)((?:["']?(?:password|passwd|secret|token|api[_-]?key|access[_-]?key|private[_-]?key)["']?)\s*[:=]\s*)("(?:\\.|[^"\\])*"|'[^']*'|[^\s,;&}]+)`)
var flags = regexp.MustCompile(`(?i)(--(?:password|passwd|secret|token|api[-_]key|access[-_]key|private[-_]key|key)(?:=|\s+))("(?:\\.|[^"\\])*"|'[^']*'|[^\s]+)`)
var userinfo = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://)([^\s/]+)@`)

type Redactor struct {
	values   []string
	replacer *strings.Replacer
}

func SensitiveName(name string) bool { return namePattern.MatchString(name) }
func New(values ...string) *Redactor {
	set := map[string]bool{}
	for _, value := range values {
		if value == "" {
			continue
		}
		for _, part := range append([]string{value}, strings.Split(value, "\n")...) {
			if part == "" {
				continue
			}
			set[part] = true
			encoded, _ := json.Marshal(part)
			set[string(encoded[1:len(encoded)-1])] = true
			set[url.QueryEscape(part)] = true
			set[url.PathEscape(part)] = true
		}
	}
	out := &Redactor{}
	for value := range set {
		out.values = append(out.values, value)
	}
	sort.Slice(out.values, func(i, j int) bool { return len(out.values[i]) > len(out.values[j]) })
	pairs := make([]string, 0, len(out.values)*2)
	for _, value := range out.values {
		pairs = append(pairs, value, Marker)
	}
	out.replacer = strings.NewReplacer(pairs...)
	return out
}
func Environment() *Redactor {
	var values []string
	for _, entry := range os.Environ() {
		name, value, _ := strings.Cut(entry, "=")
		if SensitiveName(name) {
			values = append(values, value)
		}
	}
	return New(values...)
}
func (r *Redactor) Text(text string) string {
	text = r.replacer.Replace(text)
	text = userinfo.ReplaceAllString(text, "${1}"+Marker+"@")
	text = assignments.ReplaceAllString(text, "${1}"+Marker)
	return flags.ReplaceAllString(text, "${1}"+Marker)
}

// JSON preserves JSON types and escaping while sanitizing string leaves and keys.
func (r *Redactor) JSON(data []byte) ([]byte, error) {
	var value interface{}
	d := json.NewDecoder(strings.NewReader(string(data)))
	d.UseNumber()
	if err := d.Decode(&value); err != nil {
		return nil, err
	}
	values := append([]string{}, r.values...)
	collectSecrets(value, &values)
	r = New(values...)
	var visit func(interface{}) interface{}
	visit = func(v interface{}) interface{} {
		switch x := v.(type) {
		case string:
			return r.Text(x)
		case []interface{}:
			for i := range x {
				x[i] = visit(x[i])
			}
		case map[string]interface{}:
			out := map[string]interface{}{}
			for k, item := range x {
				if secretField(k) {
					out[r.Text(k)] = Marker
				} else {
					out[r.Text(k)] = visit(item)
				}
			}
			return out
		}
		return v
	}
	return json.MarshalIndent(visit(value), "", "  ")
}

// Stream buffers at most one line. Oversized lines are discarded in full;
// emitting their prefixes could expose a secret split at the buffer boundary.
type Stream struct {
	mu          sync.Mutex
	destination io.Writer
	redactor    *Redactor
	pending     []byte
	dropping    bool
}

func NewStream(w io.Writer, r *Redactor) *Stream { return &Stream{destination: w, redactor: r} }
func (s *Stream) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, b := range p {
		if s.dropping {
			if b == '\n' {
				s.dropping = false
			}
			continue
		}
		s.pending = append(s.pending, b)
		if len(s.pending) > MaxLine {
			s.pending = nil
			s.dropping = true
			if err := s.emit("[REDACTED: oversized diagnostic line]\n"); err != nil {
				return 0, err
			}
			continue
		}
		if b == '\n' {
			if err := s.emit(s.redactor.Text(string(s.pending))); err != nil {
				return 0, err
			}
			s.pending = nil
		}
	}
	return len(p), nil
}
func (s *Stream) emit(text string) error {
	n, err := io.WriteString(s.destination, text)
	if err == nil && n != len(text) {
		err = io.ErrShortWrite
	}
	return err
}
func (s *Stream) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.pending) == 0 {
		return nil
	}
	text := s.redactor.Text(string(s.pending))
	s.pending = nil
	return s.emit(text)
}

func secretField(key string) bool {
	key = strings.ToLower(strings.ReplaceAll(key, "-", "_"))
	for _, name := range []string{"password", "passwd", "secret", "token", "api_key", "access_key", "private_key", "authorization"} {
		if key == name || strings.HasSuffix(key, "_"+name) {
			return true
		}
	}
	return false
}

// Command includes recognized argument secrets in the output canary set. It does
// not change the arguments passed to the executable.
func Command(args []string) *Redactor {
	r := Environment()
	values := append([]string{}, r.values...)
	for i, arg := range args {
		for _, match := range userinfo.FindAllStringSubmatch(arg, -1) {
			if u, err := url.Parse(match[0] + "placeholder.invalid"); err == nil && u.User != nil {
				if password, ok := u.User.Password(); ok {
					values = append(values, password)
				}
			}
		}
		for _, match := range assignments.FindAllStringSubmatch(arg, -1) {
			value := strings.Trim(match[2], "\"'")
			values = append(values, value)
		}
		key, value, has := strings.Cut(arg, "=")
		if strings.HasPrefix(key, "--") && (secretField(strings.TrimPrefix(key, "--")) || strings.EqualFold(key, "--key")) {
			if has {
				values = append(values, value)
			} else if i+1 < len(args) {
				values = append(values, args[i+1])
			}
		}
		if u, err := url.Parse(arg); err == nil && u.Host != "" {
			if u.User != nil {
				if password, ok := u.User.Password(); ok {
					values = append(values, password)
				}
			}
			for key, items := range u.Query() {
				if secretField(key) {
					values = append(values, items...)
				}
			}
		}
		var payload map[string]interface{}
		if json.Unmarshal([]byte(arg), &payload) == nil {
			collectFieldSecrets(payload, &values, 0)
		}
	}
	return New(values...)
}

func collectSecrets(value interface{}, values *[]string) {
	switch x := value.(type) {
	case map[string]interface{}:
		for key, item := range x {
			if secretField(key) {
				if text, ok := item.(string); ok {
					*values = append(*values, text)
				}
			}
			collectSecrets(item, values)
		}
	case []interface{}:
		var args []string
		for _, item := range x {
			if text, ok := item.(string); ok {
				args = append(args, text)
			}
			collectSecrets(item, values)
		}
		if len(args) > 0 {
			r := Command(args)
			*values = append(*values, r.values...)
		}
	}
}

func collectFieldSecrets(value interface{}, values *[]string, depth int) {
	if depth > 32 {
		return
	}
	switch x := value.(type) {
	case map[string]interface{}:
		for key, item := range x {
			if secretField(key) {
				if text, ok := item.(string); ok {
					*values = append(*values, text)
				}
			}
			collectFieldSecrets(item, values, depth+1)
		}
	case []interface{}:
		for _, item := range x {
			collectFieldSecrets(item, values, depth+1)
		}
	}
}
