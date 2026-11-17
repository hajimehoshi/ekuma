// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package main

import "context"

var Report = report

var ErrBaselineFailed = errBaselineFailed

func RunSuite(ctx context.Context, bin string, names, engines []string, dir string) error {
	return runSuite(ctx, bin, suite{
		name:      "Example",
		benchtime: "1x",
	}, names, engines, 2, nil, dir)
}
