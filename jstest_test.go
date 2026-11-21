// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma_test

import (
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
	IsEnteredRealm:           ekuma.IsEnteredRealm,
	ParseScriptValue:         ekuma.ParseScriptValue,
	InstallGlobalProxy:       ekuma.InstallGlobalProxy,
	DetachGlobalProxy:        ekuma.DetachGlobalProxy,
	CanAccessReceiver:        ekuma.CanAccessReceiver,
	InvokeInRealm:            ekuma.InvokeInRealm,
	DeferUnhandledRejections: ekuma.DeferUnhandledRejections,
	TakeUnhandledRejection:   ekuma.TakeUnhandledRejection,
}

func TestTest262(t *testing.T) {
	test262Runner.Run(t)
}

func TestScripts(t *testing.T) {
	test262Runner.RunScripts(t, "testdata/scripts")
}

func TestLocalScriptHarness(t *testing.T) {
	test262Runner.CheckLocalScriptHarness(t)
}

func TestSkipDirectories(t *testing.T) {
	jstest.CheckSkipDirectories(t)
}

func TestTest262ExpectedFailureWithMissingInclude(t *testing.T) {
	test262Runner.CheckExpectedFailureWithMissingInclude(t)
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

func TestV8ShellOutput(t *testing.T) {
	v8Runner.CheckShellOutput(t)
}

func TestV8Quit(t *testing.T) {
	v8Runner.CheckQuit(t)
}

func TestV8Timeouts(t *testing.T) {
	v8Runner.CheckTimeouts(t)
}

func TestV8Realm(t *testing.T) {
	v8Runner.CheckRealm(t)
}

func TestV8RealmDetachGlobal(t *testing.T) {
	v8Runner.CheckRealmDetachGlobal(t)
}

func TestV8RealmNavigation(t *testing.T) {
	v8Runner.CheckRealmNavigation(t)
}

func TestV8RealmSwitch(t *testing.T) {
	v8Runner.CheckRealmSwitch(t)
}
