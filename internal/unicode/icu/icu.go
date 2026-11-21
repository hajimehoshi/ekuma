// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package icu provides ICU4C source files to Ekuma's data generators.
package icu

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
)

// Version is the ICU4C release used by the generators.
const Version = "79.1"

var checksums = map[string]string{
	"icu4c/source/data/brkitr/dictionaries/cjdict.txt":   "e73fd72048981d0cc13e9dc436a7eaba07ffb6eff58c8a59dc75c1df746663a0",
	"icu4c/source/data/brkitr/dictionaries/thaidict.txt": "3166abde40c0f44ab91c28f5ce96d7d1472cb7882e1c0bda0a72f8f69dba4274",
	"icu4c/source/i18n/persncal.cpp":                     "31e7e93bb7dc1e4436c58ffd44fd1ed2686ceb79228b8d989f6573ec15ed5f45",
}

// ReadFile returns the contents of a checksum-pinned file from the ICU repository.
func ReadFile(path string) ([]byte, error) {
	want, ok := checksums[path]
	if !ok {
		return nil, fmt.Errorf("no ICU checksum for %s", path)
	}
	url := "https://raw.githubusercontent.com/unicode-org/icu/release-" + Version + "/" + path
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(b)
	if hex.EncodeToString(sum[:]) != want {
		return nil, fmt.Errorf("%s: unexpected SHA-256 %x", url, sum)
	}
	return b, nil
}
