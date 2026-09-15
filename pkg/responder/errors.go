package responder

import (
	"errors"
	"net/http"

	"github.com/riipandi/tango/pkg/validate"
)

// StatusedError is an error carrying its HTTP status. Wrapped chains
// keep it discoverable via errors.As.
type StatusedError interface {
	error
	HTTPStatus() int
}

// statused is the StatusedError implementation for module sentinels.
type statused struct {
	status int
	msg    string
}

// NewError builds a sentinel with its HTTP status attached.
func NewError(status int, msg string) error {
	return &statused{status: status, msg: msg}
}

func (e *statused) Error() string   { return e.msg }
func (e *statused) HTTPStatus() int { return e.status }

// WriteError maps an error onto the response envelope: validation
// errors → 422 with field errors, StatusError → its status (404 keeps
// the fixed "not found" message), everything else → 500 "internal
// error". It replaces the per-module writeError switches.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	if validate.IsValidationError(err) {
		Fail(w, r, http.StatusUnprocessableEntity, "validation failed",
			WithError(validate.FieldErrors(err)))
		return
	}

	if se, ok := errors.AsType[StatusedError](err); ok {
		if se.HTTPStatus() == http.StatusNotFound {
			NotFoundJSON(w, r)
			return
		}
		Fail(w, r, se.HTTPStatus(), se.Error())
		return
	}

	Fail(w, r, http.StatusInternalServerError, "internal error")
}
