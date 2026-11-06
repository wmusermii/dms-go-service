package domain

// Kind classifies an application error. The HTTP layer maps Kind to a status
// code, so services never need to know about HTTP.
type Kind int

const (
	KindInvalid Kind = iota + 1
	KindUnauthorized
	KindNotFound
	KindConflict
	KindNotAvailable
	KindTooLarge
	KindUnsupported
	KindInternal
)

// AppError is the error type returned by services and repositories.
type AppError struct {
	Kind    Kind
	Code    string
	Message string
	Err     error
}

func (e *AppError) Error() string {
	if e.Err != nil {
		return e.Code + ": " + e.Message + ": " + e.Err.Error()
	}
	return e.Code + ": " + e.Message
}

func (e *AppError) Unwrap() error { return e.Err }

func Invalid(code, msg string) *AppError {
	return &AppError{Kind: KindInvalid, Code: code, Message: msg}
}

func Unauthorized(msg string) *AppError {
	return &AppError{Kind: KindUnauthorized, Code: "unauthorized", Message: msg}
}

func NotFound(msg string) *AppError {
	return &AppError{Kind: KindNotFound, Code: "not_found", Message: msg}
}

func Conflict(code, msg string) *AppError {
	return &AppError{Kind: KindConflict, Code: code, Message: msg}
}

// NotAvailable is returned when a document exists but cannot be served yet
// (still being scanned) or at all (infected / scan error).
func NotAvailable(msg string) *AppError {
	return &AppError{Kind: KindNotAvailable, Code: "document_not_available", Message: msg}
}

func TooLarge(msg string) *AppError {
	return &AppError{Kind: KindTooLarge, Code: "file_too_large", Message: msg}
}

func Unsupported(code, msg string) *AppError {
	return &AppError{Kind: KindUnsupported, Code: code, Message: msg}
}

// Internal wraps an unexpected error. The message returned to clients is
// generic; the wrapped error is only logged.
func Internal(err error) *AppError {
	return &AppError{Kind: KindInternal, Code: "internal_error", Message: "internal server error", Err: err}
}
