// Package validate standardizes request-body decoding and validation:
// JSON decode via encoding/json/v2 followed by the payload's own
// Validate() rules (ozzo-validation, code-first). Errors map to a
// stable field-error shape for the responder envelope.
package validate

import (
	"errors"
	"io"

	jsonv2 "encoding/json/v2"

	"github.com/go-ozzo/ozzo-validation/v4"
)

// FieldError locates one failed rule on the request payload.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// validatable is the contract every request payload may implement:
// ozzo-validation's code-first rules.
type validatable interface {
	Validate() error
}

// Request decodes the JSON body into dst and runs dst's Validate()
// when implemented. Malformed JSON and failed rules both come back
// as the returned error; use FieldErrors to split them apart.
func Request(r io.Reader, dst any) error {
	if err := jsonv2.UnmarshalRead(r, dst); err != nil {
		return err
	}
	if v, ok := dst.(validatable); ok {
		return v.Validate()
	}
	return nil
}

// IsValidationError reports whether err carries rule violations
// (validation.Errors) rather than a JSON decode failure, so
// handlers can answer 422 instead of 400.
func IsValidationError(err error) bool {
	var errs validation.Errors
	return errors.As(err, &errs)
}

// FieldErrors flattens a validation error into per-field entries.
// A non-validation error yields a single anonymous entry, keeping
// the envelope shape uniform.
func FieldErrors(err error) []FieldError {
	if err == nil {
		return nil
	}

	var errs validation.Errors
	if errors.As(err, &errs) {
		out := make([]FieldError, 0, len(errs))
		for field, fieldErr := range errs {
			out = append(out, FieldError{Field: field, Message: fieldErr.Error()})
		}
		return out
	}

	return []FieldError{{Field: "body", Message: "malformed JSON"}}
}
