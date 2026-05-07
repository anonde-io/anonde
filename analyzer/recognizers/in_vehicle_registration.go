package recognizers

import "strings"

// NewINVehicleRegistrationRecognizer detects Indian vehicle registration numbers.
// Pattern: 2 letters (state) + 1-2 digits (RTO) + optional 1-3 letters (series) + 4 digits.
// Examples: "MH 12 AB 1234", "DL01CA1234", "KA 03 N 1234".
func NewINVehicleRegistrationRecognizer() *validatedRecognizer {
	r := NewValidatedRecognizer(
		"INVehicleRegistrationRecognizer",
		"IN_VEHICLE_REGISTRATION",
		[]string{"en"},
		[][2]any{
			{`\b[A-Za-z]{2}[\s-]?\d{1,2}[\s-]?[A-Za-z]{1,3}[\s-]?\d{4}\b`, 0.5},
		},
		[]string{"vehicle", "registration", "rc number", "number plate", "vehicle number", "license plate"},
	)
	r.Normalize = func(s string) string { return strings.ToUpper(stripSeparators(s)) }
	return r
}
