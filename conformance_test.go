// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma_test

import (
	"os"
	"testing"

	"github.com/hajimehoshi/ekuma"
	"github.com/hajimehoshi/ekuma/internal/jstest"
)

var test262Runner = jstest.Test262{
	NewHTMLDDA:           ekuma.NewHTMLDDA,
	AbstractModuleSource: ekuma.AbstractModuleSource,
	NewSourceModule:      ekuma.NewSourceModule,
}

var v8Runner = jstest.V8{
	ParseScriptValue:   ekuma.ParseScriptValue,
	InstallGlobalProxy: ekuma.InstallGlobalProxy,
}

var localHarness = os.DirFS("testdata/harness")

func TestTest262(t *testing.T) {
	test262Runner.Run(t)
}

func TestScripts(t *testing.T) {
	test262Runner.RunScripts(t, "testdata/scripts", localHarness)
}

func TestLocalScriptHarness(t *testing.T) {
	test262Runner.CheckLocalScriptHarness(t, localHarness)
}

func TestTest262ExpectedFailureWithMissingInclude(t *testing.T) {
	test262Runner.CheckExpectedFailureWithMissingInclude(t, localHarness)
}

func TestV8(t *testing.T) {
	v8Runner.Run(t)
}

func TestV8FixedTime(t *testing.T) {
	v8Runner.CheckFixedTime(t)
}

func TestV8FilesOrder(t *testing.T) {
	v8Runner.CheckFilesOrder(t)
}

func TestV8ScriptLoader(t *testing.T) {
	jstest.CheckV8ScriptLoader(t)
}

func TestV8Realm(t *testing.T) {
	v8Runner.CheckRealm(t)
}
