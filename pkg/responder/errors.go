package responder

import (
	"net/http"

	"github.com/riipandi/tango/pkg/validate"
)

// WriteError maps an error to the standard response envelope.
// Validation errors become 422; anything else is an opaque 500 —
// module handlers map their own domain errors to specific statuses.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	if validate.IsValidationError(err) {
		Fail(w, r, http.StatusUnprocessableEntity, "validation failed",
			WithError(validate.FieldErrors(err)))
		return
	}

	Fail(w, r, http.StatusInternalServerError, "internal error")
}
