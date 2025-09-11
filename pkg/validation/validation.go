package validation

import (
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"
)

var v *validator.Validate

func init() {
	v = validator.New()
	// Use field name from json tag (instead of struct field name)
	v.RegisterTagNameFunc(func(fld reflect.StructField) string {
		name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
		if name == "" || name == "-" {
			return fld.Name
		}
		return name
	})
}

// Validate returns map[field][]messages similar to Laravel style
func Validate(s any) (map[string][]string, error) {
	if err := v.Struct(s); err != nil {
		ve, ok := err.(validator.ValidationErrors)
		if !ok {
			return nil, err
		}
		out := make(map[string][]string)
		for _, e := range ve {
			field := strings.ToLower(e.Field())
			switch e.Tag() {
			case "required":
				out[field] = append(out[field], "This field is required")
			case "email":
				out[field] = append(out[field], "Invalid email format")
			case "max":
				out[field] = append(out[field], "Exceeds maximum limit")
			case "min":
				out[field] = append(out[field], "Below minimum limit")
			case "oneof":
				out[field] = append(out[field], "Value is not allowed")
			case "uuid4":
				out[field] = append(out[field], "Invalid UUID format")
			case "gte":
				out[field] = append(out[field], "Must be greater than or equal to the limit")
			case "lte":
				out[field] = append(out[field], "Must be less than or equal to the limit")
			default:
				out[field] = append(out[field], e.Error())
			}
		}
		return out, nil
	}
	return nil, nil
}
