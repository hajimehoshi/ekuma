// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package comparison

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
)

// QuickJSRepository is the repository of QuickJS in C, which modernc.org/quickjs translates
// into Go.
const QuickJSRepository = "github.com/bellard/quickjs"

// QuickJSVersion is the version of QuickJS in C as a pseudo-version of its repository: the
// release 2026-06-04, which modernc.org/libquickjs v0.14.0 translates.
const QuickJSVersion = "v0.0.0-20260604125329-3d5e064e9dd6"

const (
	quickJSCommit = "3d5e064e9dd67c70f7962836505a7fa067bf0a4e"
	quickJSSum    = "afccd11533e21a4e49af43e6909fc7236ed0b92f25e01e1061ceb0b47e15e05d"
)

// QuickJSPath returns the path of the qjs command of QuickJS in C under root, which
// BuildQuickJS builds.
func QuickJSPath(root string) string {
	return filepath.Join(root, "quickjs-"+quickJSCommit[:12], "qjs")
}

// BuildQuickJS builds the qjs command of QuickJS in C under root, unless it is built already.
// It needs make and a C compiler.
func BuildQuickJS(ctx context.Context, root string) error {
	bin := QuickJSPath(root)
	if _, err := os.Stat(bin); err == nil {
		return nil
	}

	url := "https://" + QuickJSRepository + "/archive/" + quickJSCommit + ".tar.gz"
	b, err := download(ctx, url)
	if err != nil {
		return err
	}
	if got := sha256Hex(b); got != quickJSSum {
		return fmt.Errorf("comparison: %s has the SHA-256 sum %s, want %s", url, got, quickJSSum)
	}

	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	src, err := os.MkdirTemp(root, "quickjs-build-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(src)
	if err := extractTarGz(b, src); err != nil {
		return fmt.Errorf("comparison: extracting %s: %w", url, err)
	}
	cmd := exec.CommandContext(ctx, "make", "-C", src, "qjs")
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("comparison: building QuickJS: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		return err
	}
	return os.Rename(filepath.Join(src, "qjs"), bin)
}

// extractTarGz extracts the gzipped tar archive b into dir, without the top directory that the
// paths of its files start with.
func extractTarGz(b []byte, dir string) error {
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return err
	}
	tr := tar.NewReader(zr)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if h.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		_, name, _ := strings.Cut(path.Clean(h.Name), "/")
		if name == "" {
			continue
		}
		if !filepath.IsLocal(name) {
			return fmt.Errorf("the path %s is outside the archive", h.Name)
		}
		p := filepath.Join(dir, filepath.FromSlash(name))
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(p, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return err
			}
			if err := writeFile(p, tr, h.FileInfo().Mode().Perm()); err != nil {
				return err
			}
		default:
			return fmt.Errorf("the file %s is of the unexpected type %q", h.Name, h.Typeflag)
		}
	}
}

func writeFile(path string, r io.Reader, perm os.FileMode) (err error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	_, err = io.Copy(f, r)
	return err
}
