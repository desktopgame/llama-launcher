package util

import (
	"fmt"
	"strings"
)

// ValidateName checks that a name is safe for use as a filename component.
func ValidateName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("name is required")
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("name must not contain path separators")
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("name must not contain '..'")
	}
	return nil
}
