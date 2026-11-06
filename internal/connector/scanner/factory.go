package scanner

import "fmt"

// New returns the scanner selected by SCANNER_DRIVER. A nil Scanner (with a
// nil error) means scanning is disabled: documents stay PENDING and are never
// served.
//
// To add an engine: implement Scanner (e.g. clamav.go) and add a case here.
func New(driver string) (Scanner, error) {
	switch driver {
	case "disabled", "":
		return nil, nil
	case "noop":
		return Noop{}, nil
	default:
		return nil, fmt.Errorf("unknown scanner driver %q (implement it in internal/connector/scanner and register it in factory.go)", driver)
	}
}
