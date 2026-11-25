// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"github.com/hajimehoshi/ekuma/internal/quiet"
)

// Quiet returns the quiet context of the realm of cx.
func (cx *ExecutionContext) Quiet() quiet.Context {
	return quiet.Context{
		Realm: cx.realmPart,
	}
}

// quietJobs provides the promise operations that run no script or host code. Their context stays
// behind it, as they need the jobs of the agent.
type quietJobs struct {
	cx *ExecutionContext
}

// quietJobs returns the quiet promise operations of cx.
func (cx *ExecutionContext) quietJobs() quietJobs {
	return quietJobs{
		cx: cx,
	}
}
