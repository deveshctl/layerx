package theme

import "fmt"

// ErrUnknownTheme is returned by Get when no theme matches the given name.
// The wrapped string is the user-supplied name; cmd/ uses it to format the
// "unknown theme %q; run `layerx themes` to list" message.
type ErrUnknownTheme struct {
	Name string
}

func (e *ErrUnknownTheme) Error() string {
	return fmt.Sprintf("unknown theme %q", e.Name)
}
