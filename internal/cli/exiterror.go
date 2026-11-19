package cli

import "fmt"

// ExitError reports a process exit code that should be propagated by the CLI.
// Used when an executed shell command fails and dmsh should reflect its status.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil {
		return fmt.Sprintf("exit %d: %v", e.Code, e.Err)
	}
	return fmt.Sprintf("exit %d", e.Code)
}

func (e *ExitError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *ExitError) ExitCode() int {
	if e == nil || e.Code == 0 {
		return 1
	}
	return e.Code
}
