package core

import "fmt"

// APIError represents a client-facing error with a stable machine-readable reason code.
type APIError struct {
	HTTPStatus int
	ErrorCode  string
	Reason     string
	Message    string
	Meta       map[string]interface{}
}

func (e *APIError) Error() string {
	if e.Reason == "" {
		return e.Message
	}
	return fmt.Sprintf("%s: %s", e.Reason, e.Message)
}

// Common validation reason codes.
const (
	ReasonInvalidLimit            = "INVALID_LIMIT"
	ReasonInvalidStart            = "INVALID_START"
	ReasonInvalidParam            = "INVALID_PARAM"
	ReasonEmptyQuery              = "EMPTY_QUERY"
	ReasonNoEngines               = "NO_ENGINES"
	ReasonUnknownFormat           = "UNKNOWN_FORMAT"
	ReasonRequestProxyURLDisabled = "REQUEST_PROXY_URL_DISABLED"
	ReasonUnsupportedProxyScheme  = "UNSUPPORTED_PROXY_SCHEME"
)

func errInvalidLimit(msg string) *APIError {
	return &APIError{HTTPStatus: 400, Reason: ReasonInvalidLimit, Message: msg}
}

func errInvalidStart(msg string) *APIError {
	return &APIError{HTTPStatus: 400, Reason: ReasonInvalidStart, Message: msg}
}

func errInvalidParam(msg string) *APIError {
	return &APIError{HTTPStatus: 400, Reason: ReasonInvalidParam, Message: msg}
}

func errEmptyQuery() *APIError {
	return &APIError{HTTPStatus: 400, Reason: ReasonEmptyQuery, Message: "query cannot be empty: provide text, site, or file parameter"}
}
