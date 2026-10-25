package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"

	"platformctl/internal/apperror"
	"platformctl/internal/redact"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// usageError marks only errors produced by command lookup, flag parsing or
// argument validation. Runtime/provider error text never determines an exit code.
type usageError struct{ error }

func usageFailure(err error) error {
	return &usageError{apperror.New(apperror.CategoryConfig, "PLATFORMCTL_USAGE", err.Error())}
}

func (e *usageError) Unwrap() error { return e.error }

type checkedWriter struct {
	w   io.Writer
	err error
}

func (w *checkedWriter) Write(data []byte) (int, error) {
	n, err := w.w.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if w.err == nil {
		w.err = err
	}
	return n, err
}

// runCLI owns the process contract. Commands build their existing payloads;
// only this boundary publishes a JSON document or selects a process exit code.
func runCLI(root *cobra.Command, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	options.JSON = jsonRequested(root, args)
	machine := options.JSON
	var document bytes.Buffer
	out := &checkedWriter{w: stdout}
	diagnostics := &checkedWriter{w: stderr}
	sanitizer := redact.Environment()
	human := redact.NewStream(out, sanitizer)
	diagnosticStream := redact.NewStream(diagnostics, sanitizer)
	defer human.Flush()
	defer diagnosticStream.Flush()
	root.SetArgs(args)
	root.SetIn(stdin)
	root.SetErr(diagnosticStream)
	if machine {
		root.SetOut(&document)
	} else {
		root.SetOut(human)
	}
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usageFailure(err) })
	root.SetHelpCommand(&cobra.Command{
		Use: "help [command]", Short: "Help about any command",
		Args: cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			target, rest, err := root.Find(args)
			if err != nil {
				return usageFailure(err)
			}
			if len(rest) != 0 {
				return usageFailure(errors.New("unknown help topic"))
			}
			return target.Help()
		},
	})
	root.InitDefaultHelpCmd()
	for _, child := range root.Commands() {
		if child.Args == nil {
			child.Args = func(cmd *cobra.Command, args []string) error {
				if err := cobra.NoArgs(cmd, args); err != nil {
					return usageFailure(err)
				}
				return nil
			}
		}
	}
	defaultHelp := root.HelpFunc()
	root.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		if !machine {
			defaultHelp(cmd, args)
			return
		}
		var help bytes.Buffer
		cmd.SetOut(&help)
		defaultHelp(cmd, args)
		cmd.SetOut(&document)
		// This payload contains strings only; the destination is an in-memory buffer.
		_ = writeJSON(&document, map[string]interface{}{"ok": true, "help": help.String()})
	})
	_, _, lookupErr := root.Find(args)
	_, err := root.ExecuteC()
	if err != nil && lookupErr != nil {
		err = usageFailure(lookupErr)
	}
	if err == nil && machine && !json.Valid(document.Bytes()) {
		err = apperror.New(apperror.CategoryInternal, "PLATFORMCTL_OUTPUT_ENCODE", "command did not produce one valid JSON document")
	}
	code := 0
	if err != nil {
		code = 1
		var usage *usageError
		if errors.As(err, &usage) {
			code = 2
		}
		if machine {
			document.Reset()
			_ = writeJSON(&document, map[string]interface{}{"ok": false, "error": apperror.Normalize(err)})
		} else {
			apperror.Render(diagnosticStream, err, options.Verbose, false)
		}
	}
	if machine {
		// Child workflow streams already go to stderr. The result is emitted once;
		// a broken stdout is reported on stderr and is never retried as JSON.
		if _, writeErr := out.Write(document.Bytes()); writeErr != nil {
			apperror.Render(diagnosticStream, apperror.Wrap(writeErr, apperror.CategoryInternal, "PLATFORMCTL_OUTPUT_WRITE", "could not write command output"), options.Verbose, false)
			return 1
		}
	}
	_ = human.Flush()
	_ = diagnosticStream.Flush()
	if out.err != nil {
		apperror.Render(diagnosticStream, apperror.Wrap(out.err, apperror.CategoryInternal, "PLATFORMCTL_OUTPUT_WRITE", "could not write command output"), options.Verbose, false)
		return 1
	}
	if diagnostics.err != nil {
		return 1
	}
	return code
}

// Recover output-format intent even when Cobra stops before parsing --json
// (unknown command/flag). Respect value-taking flags and the -- separator. This
// does not validate commands or flags; Cobra remains the only command parser.
func jsonRequested(root *cobra.Command, args []string) bool {
	values := map[string]bool{}
	target, _, _ := root.Find(args)
	if target == nil {
		target = root
	}
	visit := func(f *pflag.Flag) {
		if f.NoOptDefVal == "" {
			values["--"+f.Name] = true
			if f.Shorthand != "" {
				values["-"+f.Shorthand] = true
			}
		}
	}
	target.LocalNonPersistentFlags().VisitAll(visit)
	target.PersistentFlags().VisitAll(visit)
	target.InheritedFlags().VisitAll(visit)
	requested := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		if values[arg] {
			i++
			continue
		}
		if arg == "--json" {
			requested = true
		}
		if value, ok := strings.CutPrefix(arg, "--json="); ok {
			parsed, err := strconv.ParseBool(value)
			// An invalid value is still a request for a machine-readable usage error.
			requested = err != nil || parsed
		}
	}
	return requested
}
