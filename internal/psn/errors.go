package psn

import "errors"

// Kind is a stable lookup failure. Values map 1:1 to user-facing Chinese copy.
type Kind string

const (
	KindNotFound           Kind = "not_found"
	KindPrivate            Kind = "private"
	KindNoCredentials      Kind = "no_credentials"
	KindInvalidCredentials Kind = "invalid_credentials"
	KindUpstream           Kind = "upstream"
)

// Error is a typed lookup failure. Error() never includes tokens or upstream bodies.
type Error struct {
	Kind Kind
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return string(e.Kind)
}

func (e *Error) Is(target error) bool {
	var other *Error
	if !errors.As(target, &other) {
		return false
	}
	return e.Kind == other.Kind
}

func kindErr(k Kind) *Error {
	return &Error{Kind: k}
}
