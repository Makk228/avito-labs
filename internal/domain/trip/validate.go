package trip

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"
)

// Validate checks the declarative domain constraints without infrastructure.
func (n NewTrip) Validate() error {
	rules := validator.New(validator.WithRequiredStructEnabled())
	rules.RegisterTagNameFunc(func(field reflect.StructField) string {
		return field.Tag.Get("json")
	})

	err := rules.Struct(n)
	if err == nil {
		return nil
	}

	var fields validator.ValidationErrors
	if !errors.As(err, &fields) {
		return fmt.Errorf("validate trip: %w", err)
	}

	first := fields[0]
	return &ValidationError{
		Field:   strings.TrimPrefix(first.Namespace(), "NewTrip."),
		Message: validationMessage(first),
	}
}

func validationMessage(field validator.FieldError) string {
	switch field.StructField() {
	case "UserID", "DriverID":
		return "must not be empty"
	case "Latitude":
		return "must be between -90 and 90"
	case "Longitude":
		return "must be between -180 and 180"
	case "Price":
		return "must be greater than or equal to 0"
	default:
		return "is invalid"
	}
}
