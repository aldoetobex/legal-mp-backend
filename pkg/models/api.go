// pkg/models/api.go
package models

// Respons error validasi gaya Laravel
type ValidationErrorResponse struct {
	Message string              `json:"message" example:"Validation failed"`
	Errors  map[string][]string `json:"errors"`
}

// Respons error umum (403/404/409/500)
type ErrorResponse struct {
	Error   bool   `json:"error" example:"true"`
	Message string `json:"message" example:"Forbidden"`
	Code    string `json:"code,omitempty" example:"FORBIDDEN"`
}
