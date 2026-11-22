// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package cldrgen supports generation of tables from CLDR releases.
package cldrgen

import (
	"fmt"
	"os"
	"strings"
)

// Copyright returns the CLDR copyright holder and years after verifying the local license and notice.
func Copyright(releaseLicense []byte, licensePath, noticePath string) (string, error) {
	text := string(releaseLicense)
	var copyright string
	for line := range strings.Lines(text) {
		if c, ok := strings.CutPrefix(strings.TrimSpace(line), "Copyright © "); ok {
			copyright = c
			break
		}
	}
	// The release also covers UTS #35. The data license starts at its notice to the user.
	start := strings.Index(text, "NOTICE TO USER:")
	end := strings.Index(text, "SPDX-License-Identifier: Unicode-3.0")
	if copyright == "" || start < 0 || end < start {
		return "", fmt.Errorf("the CLDR release lacks the Unicode License v3")
	}
	license, err := os.ReadFile(licensePath)
	if err != nil {
		return "", err
	}
	if !strings.Contains(string(license), strings.TrimSpace(text[start:end])) {
		return "", fmt.Errorf("update %s with the CLDR release's license", licensePath)
	}
	notice, err := os.ReadFile(noticePath)
	if err != nil {
		return "", err
	}
	if !strings.Contains(string(notice), "Copyright © "+copyright+"\n") {
		return "", fmt.Errorf("update %s with the CLDR release's copyright", noticePath)
	}
	return copyright, nil
}
