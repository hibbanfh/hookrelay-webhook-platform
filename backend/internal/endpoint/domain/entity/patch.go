package entity

// Patch is a partial update. A nil field is absent and stays unchanged; a non-nil field is
// validated and applied (an empty Description clears it). Delivery rejects explicit nulls
// before a Patch is built.
type Patch struct {
	URL         *string
	Description *string
	Enabled     *bool
}

// IsEmpty reports whether the patch changes nothing.
func (p Patch) IsEmpty() bool {
	return p.URL == nil && p.Description == nil && p.Enabled == nil
}

// validate checks the fields that are present with the same rules as NewEndpoint.
func (p Patch) validate() error {
	if err := validateIfPresent(p.URL, validateURL); err != nil {
		return err
	}
	return validateIfPresent(p.Description, validateDescription)
}

func validateIfPresent(value *string, check func(string) error) error {
	if value == nil {
		return nil
	}
	return check(*value)
}
