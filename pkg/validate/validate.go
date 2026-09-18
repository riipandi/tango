// Package validate decodes and validates request bodies.
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

// validatable is implemented by payloads with validation rules.
type validatable interface {
	Validate() error
}

// Request decodes the JSON body and runs validation when supported.
func Request(r io.Reader, dst any) error {
	if err := jsonv2.UnmarshalRead(r, dst); err != nil {
		return err
	}
	if v, ok := dst.(validatable); ok {
		return v.Validate()
	}
	return nil
}

// IsValidationError reports whether err contains validation failures.
func IsValidationError(err error) bool {
	var errs validation.Errors
	return errors.As(err, &errs)
}

// FieldErrors converts an error into field-level response entries.
func FieldErrors(err error) []FieldError {
	if err == nil {
		return nil
	}

	if errs, ok := errors.AsType[validation.Errors](err); ok {
		out := make([]FieldError, 0, len(errs))
		for field, fieldErr := range errs {
			out = append(out, FieldError{Field: field, Message: fieldErr.Error()})
		}
		return out
	}

	return []FieldError{{Field: "body", Message: "malformed JSON"}}
}
