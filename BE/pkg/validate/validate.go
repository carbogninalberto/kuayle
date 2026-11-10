package validate

import (
	"regexp"

	"github.com/go-playground/validator/v10"
)

// slugPattern matches lowercase alphanumeric groups joined by single hyphens.
// Leading, trailing and consecutive hyphens are rejected.
var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

var V *validator.Validate

func init() {
	V = validator.New(validator.WithRequiredStructEnabled())
	if err := V.RegisterValidation("slug", isSlug); err != nil {
		panic(err)
	}
	if err := V.RegisterValidation("workspace_slug", func(fl validator.FieldLevel) bool {
		return !IsReservedWorkspaceSlug(fl.Field().String())
	}); err != nil {
		panic(err)
	}
}

// IsReservedWorkspaceSlug reports whether slug is a top-level path the server
// keeps for itself, so a workspace at /<slug> would lose its root page.
func IsReservedWorkspaceSlug(slug string) bool {
	return slug == "mcp"
}

func isSlug(fl validator.FieldLevel) bool {
	return slugPattern.MatchString(fl.Field().String())
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
	case "workspace_slug":
		return "is reserved; choose another"
	case "slug":
		return "must contain only lowercase letters, digits and single hyphens between them"
	default:
		return "invalid value"
	}
}
