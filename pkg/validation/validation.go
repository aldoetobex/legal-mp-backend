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

	// Bar number: 3–40 chars, alphanumerics plus space, dash, slash.
	reBarNum       = regexp.MustCompile(`^[A-Za-z0-9 /-]{3,40}$`)
	reJurisdiction = regexp.MustCompile(`^[A-Z]{2}$`) // ISO-3166 alpha-2, e.g. SG
)

func init() {
	v = validator.New()

	// Use JSON tag as the field name in error output
	v.RegisterTagNameFunc(func(fld reflect.StructField) string {
		name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
		if name == "" || name == "-" {
			return fld.Name
		}
		return name
	})

	// Custom: bar number
	_ = v.RegisterValidation("barnum", func(fl validator.FieldLevel) bool {
		val := strings.TrimSpace(fl.Field().String())
		if val == "" { // let omitempty handle empty
			return true
		}
		return reBarNum.MatchString(val)
	})

	// Custom: jurisdiction code
	_ = v.RegisterValidation("jurisdiction", func(fl validator.FieldLevel) bool {
		val := strings.TrimSpace(strings.ToUpper(fl.Field().String()))
		if val == "" {
			return true
		}
		return reJurisdiction.MatchString(val)
	})
}

// Validate returns map[field][]messages (Laravel-like)
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
				// Show a string-specific message when the field is a string
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
				out[field] = append(out[field], "Invalid jurisdiction code (use ISO-3166 alpha-2, e.g. “SG”)")

			default:
				// Fallback to original error text if we missed a tag
				out[field] = append(out[field], e.Error())
			}
		}
		return out, nil
	}
	return nil, nil
}
