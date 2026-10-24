package service

import "fmt"

// Kind classifies a Error so transports (REST, MCP) can map it to the right
// status/response without parsing message text.
type Kind string

const (
	KindNotFound Kind = "not_found"
	KindInvalid  Kind = "invalid"
	KindConflict Kind = "conflict"
	KindUpstream Kind = "upstream"
)

// Error is the one error type every service method returns for
// business-rule failures, so REST and MCP handlers share one translation to
// their own error shape.
type Error struct {
	Kind    Kind
	Message string
}

func (e *Error) Error() string {
	return e.Message
}

func newError(kind Kind, format string, args ...any) *Error {
	return &Error{Kind: kind, Message: fmt.Sprintf(format, args...)}
}

func notFoundf(format string, args ...any) *Error {
	return newError(KindNotFound, format, args...)
}

func invalidf(format string, args ...any) *Error {
	return newError(KindInvalid, format, args...)
}

func conflictf(format string, args ...any) *Error {
	return newError(KindConflict, format, args...)
}
