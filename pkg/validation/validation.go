package validation

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/go-playground/validator/v10"
)

var (
	v *validator.Validate

	// Bar number: 3–40 chars; allow letters, digits, space, dash, slash.
	reBarNum = regexp.MustCompile(`^[A-Za-z0-9 /-]{3,40}$`)
	// Jurisdiction: ISO-3166 alpha-2 (e.g., "SG").
	reJurisdiction = regexp.MustCompile(`^[A-Z]{2}$`)
)

func init() {
	v = validator.New()

	// Use the JSON tag as the field name in error messages.
	v.RegisterTagNameFunc(func(fld reflect.StructField) string {
		name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
		if name == "" || name == "-" {
			return fld.Name
		}
		return name
	})

	// Custom rule: bar number format (allows empty via `omitempty`).
	_ = v.RegisterValidation("barnum", func(fl validator.FieldLevel) bool {
		val := strings.TrimSpace(fl.Field().String())
		if val == "" {
			return true
		}
		return reBarNum.MatchString(val)
	})

	// Custom rule: jurisdiction code (allows empty via `omitempty`).
	_ = v.RegisterValidation("jurisdiction", func(fl validator.FieldLevel) bool {
		val := strings.TrimSpace(strings.ToUpper(fl.Field().String()))
		if val == "" {
			return true
		}
		return reJurisdiction.MatchString(val)
	})
}

// Validate runs struct validation and returns Laravel-like errors:
// map[field][]messages. Returns (nil, nil) when valid.
func Validate(s any) (map[string][]string, error) {
	if err := v.Struct(s); err != nil {
		ve, ok := err.(validator.ValidationErrors)
		if !ok {
			return nil, err
		}

		out := make(map[string][]string)
		for _, e := range ve {
			field := e.Field() // already mapped from json tag

			switch e.Tag() {
			case "required":
				out[field] = append(out[field], "This field is required")

			case "email":
				out[field] = append(out[field], "Invalid email format")

			case "min":
				// String vs numeric messaging
				if e.Kind() == reflect.String {
					out[field] = append(out[field], fmt.Sprintf("Must be at least %s characters", e.Param()))
				} else {
					out[field] = append(out[field], fmt.Sprintf("Must be at least %s", e.Param()))
				}

			case "max":
				if e.Kind() == reflect.String {
					out[field] = append(out[field], fmt.Sprintf("Must be at most %s characters", e.Param()))
				} else {
					out[field] = append(out[field], fmt.Sprintf("Must be at most %s", e.Param()))
				}

			case "oneof":
				out[field] = append(out[field], "Value is not allowed")

			case "uuid", "uuid4":
				out[field] = append(out[field], "Invalid UUID format")

			case "gte":
				if e.Kind() == reflect.String {
					out[field] = append(out[field], fmt.Sprintf("Must be at least %s characters", e.Param()))
				} else {
					out[field] = append(out[field], fmt.Sprintf("Must be greater than or equal to %s", e.Param()))
				}

			case "lte":
				if e.Kind() == reflect.String {
					out[field] = append(out[field], fmt.Sprintf("Must be at most %s characters", e.Param()))
				} else {
					out[field] = append(out[field], fmt.Sprintf("Must be less than or equal to %s", e.Param()))
				}

			case "barnum":
				out[field] = append(out[field], "Invalid bar number format")

			case "jurisdiction":
				out[field] = append(out[field], "Invalid jurisdiction code (use ISO-3166 alpha-2, e.g., \"SG\")")

			default:
				// Fallback to the original validation error string.
				out[field] = append(out[field], e.Error())
			}
		}
		return out, nil
	}

	// No validation errors.
	return nil, nil
}
