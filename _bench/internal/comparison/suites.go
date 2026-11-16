// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package comparison

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// A Suite is a set of benchmark programs at a fixed revision of their repository.
type Suite struct {
	// dir is the directory of the suite under the root.
	dir string

	// baseURL is the URL that the name of a file is appended to.
	baseURL string

	// sums holds the SHA-256 sums of the files of the suite, keyed by file name.
	sums map[string]string
}

// V8 is the V8 benchmark suite, version 7: the benchmarks directory of the tag 4.5.1 of
// https://github.com/v8/v8. Its files are under the licenses that their headers state.
var V8 = &Suite{
	dir:     "v8-v7",
	baseURL: "https://raw.githubusercontent.com/v8/v8/4.5.1/benchmarks/",
	sums: map[string]string{
		"base.js":          "ac383759c3cbf5f3a499ade526a4e296f7925733d8c3889b6d5840ce6867ff51",
		"crypto.js":        "d8b885eadc8be11c28f79543876beaa85bd70c71cfd5265c020005f43b59f48a",
		"deltablue.js":     "ca04067f896887da902efd7117cfb49ecc36fcad937922edca65a5de9d9fc96e",
		"earley-boyer.js":  "7bfa888839dee0939798d0f5da251f0ecb30fc5ea2ee52747b4adc5b0386e6ad",
		"navier-stokes.js": "ce241465ffeee77b5c4f7119f522aa971fe1fe92c3aed76f7514ea43e07fec84",
		"raytrace.js":      "d52183ca324cdb88d9ee3e78326433fe2ca28d6436f0a6c3ec013ab67cb798eb",
		"regexp.js":        "f04a1b4817e5d687363e99d941cc78de9b29b722b46cc034d877f59a269bdfdf",
		"richards.js":      "d86eac4108343a54ba6e10621a77b46f82f554c3b0a46e8fb4032d5cf65a2a11",
		"splay.js":         "1f875cec579d03af40d256ceaeb6823f3ef16c883652650ca51d183472a8185d",
	},
}

// Micro is the micro benchmarks of QuickJS: tests/microbench.js at the commit 888481d of
// https://github.com/bellard/quickjs, under the MIT license that its header states.
var Micro = &Suite{
	dir:     "microbench",
	baseURL: "https://raw.githubusercontent.com/bellard/quickjs/888481da0b69b369333bfcbf52156273954f8c72/tests/",
	sums: map[string]string{
		"microbench.js": "c0085abf015ffbf0a0b349d53d2f72c11e563146ee008384f1a85709907b4168",
	},
}

// Root returns the directory that holds the suites: $EKUMA_BENCH, or ekuma/bench in the user
// cache directory.
func Root() (string, error) {
	if d := os.Getenv("EKUMA_BENCH"); d != "" {
		return d, nil
	}
	c, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(c, "ekuma", "bench"), nil
}

// Load reads the files of s under root, keyed by file name. The error wraps fs.ErrNotExist when
// a file is missing.
func (s *Suite) Load(root string) (map[string]string, error) {
	srcs := make(map[string]string, len(s.sums))
	for name, sum := range s.sums {
		b, err := os.ReadFile(filepath.Join(root, s.dir, name))
		if err != nil {
			return nil, err
		}
		if got := sha256Hex(b); got != sum {
			return nil, fmt.Errorf("comparison: %s has the SHA-256 sum %s, want %s", filepath.Join(root, s.dir, name), got, sum)
		}
		srcs[name] = string(b)
	}
	return srcs, nil
}

// Fetch downloads the files of s that are missing under root or differ from their revision.
func (s *Suite) Fetch(ctx context.Context, root string) error {
	dir := filepath.Join(root, s.dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for name, sum := range s.sums {
		path := filepath.Join(dir, name)
		if b, err := os.ReadFile(path); err == nil && sha256Hex(b) == sum {
			continue
		}
		url := s.baseURL + name
		b, err := download(ctx, url)
		if err != nil {
			return err
		}
		if got := sha256Hex(b); got != sum {
			return fmt.Errorf("comparison: %s has the SHA-256 sum %s, want %s", url, got, sum)
		}
		if err := os.WriteFile(path, b, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func download(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("comparison: GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
