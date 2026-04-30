package apperror

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

type Category string

const (
	CategoryConfig     Category = "configuration"
	CategoryDependency Category = "dependency"
	CategoryExecution  Category = "execution"
	CategoryState      Category = "state"
	CategoryTemplate   Category = "template"
	CategoryInternal   Category = "internal"
)

type Error struct {
	Code        string   `json:"code"`
	Category    Category `json:"category"`
	Message     string   `json:"message"`
	Context     string   `json:"context,omitempty"`
	Remediation string   `json:"remediation,omitempty"`
	Err         error    `json:"-"`
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return e.Message
	}
	return fmt.Sprintf("%s: %v", e.Message, e.Err)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func New(category Category, code, message string) *Error {
	return &Error{Category: category, Code: code, Message: message}
}

func Wrap(err error, category Category, code, message string) error {
	if err == nil {
		return nil
	}
	var app *Error
	if errors.As(err, &app) {
		return err
	}
	return &Error{Category: category, Code: code, Message: message, Err: err}
}

func WithContext(err error, context string) error {
	var app *Error
	if errors.As(err, &app) {
		copy := *app
		copy.Context = context
		return &copy
	}
	return &Error{Category: CategoryInternal, Code: "PLATFORMCTL_INTERNAL", Message: "operation failed", Context: context, Err: err}
}

func WithRemediation(err error, remediation string) error {
	var app *Error
	if errors.As(err, &app) {
		copy := *app
		copy.Remediation = remediation
		return &copy
	}
	return &Error{Category: CategoryInternal, Code: "PLATFORMCTL_INTERNAL", Message: "operation failed", Remediation: remediation, Err: err}
}

func Render(w io.Writer, err error, verbose, asJSON bool) {
	if err == nil {
		return
	}
	app := Normalize(err)
	if asJSON {
		payload := map[string]interface{}{
			"ok":    false,
			"error": app,
		}
		_ = json.NewEncoder(w).Encode(payload)
		return
	}
	fmt.Fprintf(w, "Error [%s]: %s\n", app.Code, app.Message)
	if app.Context != "" {
		fmt.Fprintf(w, "Context: %s\n", app.Context)
	}
	if app.Remediation != "" {
		fmt.Fprintf(w, "Fix: %s\n", app.Remediation)
	}
	if verbose && app.Err != nil {
		fmt.Fprintf(w, "Cause: %v\n", app.Err)
	}
}

func Normalize(err error) *Error {
	var app *Error
	if errors.As(err, &app) {
		return app
	}
	return &Error{
		Category: CategoryInternal,
		Code:     "PLATFORMCTL_INTERNAL",
		Message:  "operation failed",
		Err:      err,
	}
}
