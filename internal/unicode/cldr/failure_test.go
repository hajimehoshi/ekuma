// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package cldr_test

import (
	"errors"
	"io"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/unicode/cldr"
	"github.com/hajimehoshi/ekuma/internal/xerrors"
)

func TestFailedBlock(t *testing.T) {
	m := cldr.BrokenMap()
	for range 2 {
		func() {
			defer func() {
				r := recover()
				err, ok := r.(*xerrors.InternalError)
				if !ok || !errors.Is(err, io.ErrUnexpectedEOF) {
					t.Errorf("Value panic: %v, want truncated block failure", r)
				}
			}()
			m.Value(0)
		}()
	}
}
