package validate

import (
	"github.com/go-playground/validator/v10"
)

var V *validator.Validate

func init() {
	V = validator.New(validator.WithRequiredStructEnabled())
}

func Struct(s interface{}) error {
	return V.Struct(s)
}

func FormatErrors(err error) []map[string]string {
	var errors []map[string]string
	if validationErrors, ok := err.(validator.ValidationErrors); ok {
		for _, e := range validationErrors {
			errors = append(errors, map[string]string{
				"field":   e.Field(),
				"message": formatMessage(e),
			})
		}
	}
	return errors
}

func formatMessage(e validator.FieldError) string {
	switch e.Tag() {
	case "required":
		return e.Field() + " is required"
	case "email":
		return "must be a valid email"
	case "min":
		return "must be at least " + e.Param() + " characters"
	case "max":
		return "must be at most " + e.Param() + " characters"
	case "uuid":
		return "must be a valid UUID"
	case "oneof":
		return "must be one of: " + e.Param()
	default:
		return "invalid value"
	}
}
