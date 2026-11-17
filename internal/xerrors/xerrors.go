// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package xerrors carries failures that cannot be handled by JavaScript.
package xerrors

// InternalError is a failure of an engine operation.
type InternalError struct {
	Op  string
	Err error
}

func (e *InternalError) Error() string {
	return "ekuma: " + e.Op + ": " + e.Err.Error()
}

func (e *InternalError) Unwrap() error {
	return e.Err
}
