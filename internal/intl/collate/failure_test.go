// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package collate_test

import (
	"bytes"
	"compress/flate"
	"errors"
	"io"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/intl/collate"
	"github.com/hajimehoshi/ekuma/internal/xerrors"
)

func TestFailedTailoring(t *testing.T) {
	var compressed bytes.Buffer
	w, err := flate.NewWriter(&compressed, flate.DefaultCompression)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		data string
		want error
	}{
		{
			name: "inflate",
			data: "",
			want: io.ErrUnexpectedEOF,
		},
		{
			name: "decode",
			data: compressed.String(),
			want: io.EOF,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var entry collate.TailoringEntry
			for range 2 {
				func() {
					defer func() {
						r := recover()
						err, ok := r.(*xerrors.InternalError)
						if !ok || !errors.Is(err, tc.want) {
							t.Errorf("Load panic: %v, want failure wrapping %v", r, tc.want)
						}
					}()
					entry.Load(tc.data)
				}()
			}
		})
	}
}
