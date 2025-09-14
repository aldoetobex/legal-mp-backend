package models

// ValidationErrorResponse is returned when input validation fails.
// Similar style to Laravel's validation error format.
type ValidationErrorResponse struct {
	Message string              `json:"message" example:"Validation failed"`
	Errors  map[string][]string `json:"errors"` // field name -> list of error messages
}

// ErrorResponse is a generic error response used for most failures
// (403 Forbidden, 404 Not Found, 409 Conflict, 500 Internal Server Error, etc).
type ErrorResponse struct {
	Error   bool   `json:"error" example:"true"`               // always true for error cases
	Message string `json:"message" example:"Forbidden"`        // human-readable error message
	Code    string `json:"code,omitempty" example:"FORBIDDEN"` // machine-friendly error code
}
