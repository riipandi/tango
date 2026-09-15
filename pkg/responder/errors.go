package responder

import (
	"errors"
	"net/http"

	"github.com/riipandi/tango/pkg/validate"
)

// StatusedError carries an HTTP status.
type StatusedError interface {
	error
	HTTPStatus() int
}

// statused attaches an HTTP status to an error message.
type statused struct {
	status int
	msg    string
}

// NewError returns an error with an HTTP status.
func NewError(status int, msg string) error {
	return &statused{status: status, msg: msg}
}

func (e *statused) Error() string   { return e.msg }
func (e *statused) HTTPStatus() int { return e.status }

// WriteError maps an error to the standard response envelope.
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
