package machine

import "fmt"

// missingImageError is why a machine image that is not on the host cannot be
// used, or nil when the runtime may pull it.
func missingImageError(reference string, pullImages bool) error {
	if !pullImages {
		return fmt.Errorf("image %s is not present and pulling is disabled", reference)
	}
	return nil
}
