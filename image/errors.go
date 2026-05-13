package image

import "fmt"

type ErrDaemonNotRunning struct {
	Cause error
}

func (e *ErrDaemonNotRunning) Error() string {
	return fmt.Sprintf("cannot connect to Docker daemon: is Docker running? (%v)", e.Cause)
}

func (e *ErrDaemonNotRunning) Unwrap() error { return e.Cause }

type ErrImageNotFound struct {
	Ref   string
	Cause error
}

func (e *ErrImageNotFound) Error() string {
	return fmt.Sprintf("image %q not found: %v", e.Ref, e.Cause)
}

func (e *ErrImageNotFound) Unwrap() error { return e.Cause }

type ErrPullFailed struct {
	Ref   string
	Cause error
}

func (e *ErrPullFailed) Error() string {
	return fmt.Sprintf("failed to pull image %q: %v", e.Ref, e.Cause)
}

func (e *ErrPullFailed) Unwrap() error { return e.Cause }
