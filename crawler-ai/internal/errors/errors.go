package apperrors

import (
	stderrors "errors"
	"fmt"
)

type Code string

const (
	CodeInvalidConfig    Code = "invalid_config"
	CodeInvalidArgument  Code = "invalid_argument"
	CodePermissionDenied Code = "permission_denied"
	CodeProviderFailed   Code = "provider_failed"
	CodePathViolation    Code = "path_violation"
	CodeStartupFailed    Code = "startup_failed"
	CodeToolFailed       Code = "tool_failed"
)

type Error struct {
	Op      string
	Code    Code
	Message string
	Err     error
}

func New(op string, code Code, message string) *Error {
	return &Error{
		Op:      op,
		Code:    code,
		Message: message,
	}
}

func Wrap(op string, code Code, err error, message string) *Error {
	return &Error{
		Op:      op,
		Code:    code,
		Message: message,
		Err:     err,
	}
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}

	if e.Err == nil {
		return fmt.Sprintf("%s: %s (%s)", e.Op, e.Message, e.Code)
	}

	return fmt.Sprintf("%s: %s (%s): %v", e.Op, e.Message, e.Code, e.Err)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.Err
}

func IsCode(err error, code Code) bool {
	var target *Error
	if !stderrors.As(err, &target) {
		return false
	}

	return target.Code == code
}
