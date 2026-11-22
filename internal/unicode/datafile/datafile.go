// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package datafile reads the Unicode and ICU release files used by Ekuma's generators.
package datafile

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"unicode"
)

// UnicodeVersion is the Unicode version shipped by the Go release in go.mod.
const UnicodeVersion = "17.0.0"

// ICUVersion is the ICU4C release used for dictionaries and calendar data.
const ICUVersion = "79.1"

var icuChecksums = map[string]string{
	"icu4c/source/data/brkitr/dictionaries/burmesedict.txt": "61d8abc3d9102b2f9bf0c9f44db0d7ab89b18172d8cd26832e4c83174bd8673b",
	"icu4c/source/data/brkitr/dictionaries/cjdict.txt":      "e73fd72048981d0cc13e9dc436a7eaba07ffb6eff58c8a59dc75c1df746663a0",
	"icu4c/source/data/brkitr/dictionaries/khmerdict.txt":   "87bee2d17cd5148aa36957eb05409eefc124de8ad519b81b789298ef3e60b5d9",
	"icu4c/source/data/brkitr/dictionaries/thaidict.txt":    "3166abde40c0f44ab91c28f5ce96d7d1472cb7882e1c0bda0a72f8f69dba4274",
	"icu4c/source/i18n/persncal.cpp":                        "31e7e93bb7dc1e4436c58ffd44fd1ed2686ceb79228b8d989f6573ec15ed5f45",
}

// ReadICU returns a source file from the pinned ICU release.
func ReadICU(path string) ([]byte, error) {
	want, ok := icuChecksums[path]
	if !ok {
		return nil, fmt.Errorf("no ICU checksum for %s", path)
	}
	url := "https://raw.githubusercontent.com/unicode-org/icu/release-" + ICUVersion + "/" + path
	return read(url, want)
}

var unicodeChecksums = map[string]string{
	"emoji/emoji-sequences.txt":                 "12cc8267dc33cbd11ed32bcf6fc5dc2ad9c7a77bae1bdfba2f41b1b9b3ead8dd",
	"emoji/emoji-zwj-sequences.txt":             "5b25441daed2322b068c5e70cda522946a4f0274df864445a1965a92e5fc5cad",
	"ucd/CaseFolding.txt":                       "ff8d8fefbf123574205085d6714c36149eb946d717a0c585c27f0f4ef58c4183",
	"ucd/DerivedCoreProperties.txt":             "24c7fed1195c482faaefd5c1e7eb821c5ee1fb6de07ecdbaa64b56a99da22c08",
	"ucd/DerivedNormalizationProps.txt":         "71fd6a206a2c0cdd41feb6b7f656aa31091db45e9cedc926985d718397f9e488",
	"ucd/LineBreak.txt":                         "e6a18fa91f8f6a6f8e534b1d3f128c21ada45bfe152eb6b1bcc5e15fd8ac92e6",
	"ucd/PropertyValueAliases.txt":              "64e9a5f76f7a1e8b5a47d6a1f9a26522a251208f5276bdfa1559dac7cf2e827a",
	"ucd/ScriptExtensions.txt":                  "ec2107e58825a1586acee8e0911ce18260394ac8b87e535ca325f1ccbeb06bc6",
	"ucd/Scripts.txt":                           "9f5e50d3abaee7d6ce09480f325c706f485ae3240912527e651954d2d6b035bf",
	"ucd/SpecialCasing.txt":                     "efc25faf19de21b92c1194c111c932e03d2a5eaf18194e33f1156e96de4c9588",
	"ucd/UnicodeData.txt":                       "2e1efc1dcb59c575eedf5ccae60f95229f706ee6d031835247d843c11d96470c",
	"ucd/auxiliary/GraphemeBreakProperty.txt":   "d6b51d1d2ae5c33b451b7ed994b48f1f4dc62b2272a5831e7fd418514a6bae89",
	"ucd/auxiliary/SentenceBreakProperty.txt":   "871c0c985ad95125e25b302414065a10839d068970bceb383ecec138f22a0a18",
	"ucd/auxiliary/WordBreakProperty.txt":       "72274cac1e6b919507db35655c3e175aa27274668a1ece95c28d2069f2ad9852",
	"ucd/emoji/emoji-data.txt":                  "2cb2bb9455cda83e8481541ecf5b6dfda66a3bb89efa3fa7c5297eccf607b72b",
	"ucd/extracted/DerivedBinaryProperties.txt": "13dd09d35a9377e33eb388a01e6581d4bfec6b2685316078c341982fa444071a",
	"ucd/extracted/DerivedGeneralCategory.txt":  "d62e5bab70ca74f099343f71224fa051cb1fdd61a1ab45c0488c44cfc0b6102e",
}

// ReadUnicode returns a data file from the pinned Unicode release.
func ReadUnicode(path string) ([]byte, error) {
	if unicode.Version != UnicodeVersion {
		return nil, fmt.Errorf("generating Unicode %s tables requires a Go toolchain with that Unicode version (this one has %s)", UnicodeVersion, unicode.Version)
	}
	want, ok := unicodeChecksums[path]
	if !ok {
		return nil, fmt.Errorf("no Unicode checksum for %s", path)
	}
	return read("https://www.unicode.org/Public/"+UnicodeVersion+"/"+path, want)
}

func read(url, want string) ([]byte, error) {
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
