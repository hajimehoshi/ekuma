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
	"icu4c/source/data/brkitr/dictionaries/cjdict.txt":        "e73fd72048981d0cc13e9dc436a7eaba07ffb6eff58c8a59dc75c1df746663a0",
	"icu4c/source/data/brkitr/dictionaries/thaidict.txt":      "3166abde40c0f44ab91c28f5ce96d7d1472cb7882e1c0bda0a72f8f69dba4274",
	"icu4c/source/data/unidata/CaseFolding.txt":               "a004797658a457bec4dc11683e39f69249ea3b595b752dbea6721c4c9f587b0d",
	"icu4c/source/data/unidata/emoji-sequences.txt":           "1823dce71f3dd9cb0ad1976797baee4ffbdc1909756f43af8ce4c57f732843a4",
	"icu4c/source/data/unidata/emoji-zwj-sequences.txt":       "f61b5213bdf85a57741a9f903cb38d0f39a3d13be6ff4aca3b39857e859a9191",
	"icu4c/source/data/unidata/DerivedCoreProperties.txt":     "5e65e031d1f986861fc1029c57e73067ead2cde62b3d25bbb4ec7ea6c8094be8",
	"icu4c/source/data/unidata/SpecialCasing.txt":             "8538dea57c184f1ef3783885ea79677b10f6efa06423717157e63712f14d1ad2",
	"icu4c/source/data/unidata/UnicodeData.txt":               "0736451de439ae7baf1425136617da495e09ee5afbe6e394374db7009ea08950",
	"icu4c/source/data/unidata/DerivedNormalizationProps.txt": "a8520eb851d4eaeb7d5a97735b4b740c9af401dee4210303ee5d9f4946f902c2",
	"icu4c/source/i18n/persncal.cpp":                          "31e7e93bb7dc1e4436c58ffd44fd1ed2686ceb79228b8d989f6573ec15ed5f45",
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
