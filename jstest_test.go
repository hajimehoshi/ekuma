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
	ObjectCreationRealm:      ekuma.ObjectCreationRealm,
	ParseScriptValue:         ekuma.ParseScriptValue,
	CallScript:               ekuma.CallScript,
	EvaluateScriptTask:       ekuma.EvaluateScriptTask,
	InstallGlobalProxy:       ekuma.InstallGlobalProxy,
	NewRealmShared:           ekuma.NewRealmShared,
	DetachGlobalProxy:        ekuma.DetachGlobalProxy,
	CanAccessReceiver:        ekuma.CanAccessReceiver,
	InvokeInRealm:            ekuma.InvokeInRealm,
	SetExceptionReporter:     ekuma.SetExceptionReporter,
	RequestInterrupt:         ekuma.RequestInterrupt,
	InterruptNow:             ekuma.InterruptNow,
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

func TestTest262EarlyNegativeWithRuntimeError(t *testing.T) {
	test262Runner.CheckEarlyNegativeWithRuntimeError(t)
}

func TestV8(t *testing.T) {
	v8Runner.Run(t)
}

func TestV8Test262(t *testing.T) {
	test262Runner.RunV8LocalTests(t)
}

func TestV8ExposeGC(t *testing.T) {
	v8Runner.CheckExposeGC(t)
}

func TestV8FixedTime(t *testing.T) {
	v8Runner.CheckFixedTime(t)
}

func TestV8FilesOrder(t *testing.T) {
	v8Runner.CheckFilesOrder(t)
}

func TestV8Bundle(t *testing.T) {
	v8Runner.CheckBundle(t)
}

func TestV8ScriptLoader(t *testing.T) {
	v8Runner.CheckScriptLoader(t)
}

func TestV8DataURL(t *testing.T) {
	v8Runner.CheckDataURL(t)
}

func TestV8ShellOutput(t *testing.T) {
	v8Runner.CheckShellOutput(t)
}

func TestV8Quit(t *testing.T) {
	v8Runner.CheckQuit(t)
}

func TestV8Termination(t *testing.T) {
	v8Runner.CheckTermination(t)
}

func TestV8Timeouts(t *testing.T) {
	v8Runner.CheckTimeouts(t)
}

func TestV8Realm(t *testing.T) {
	v8Runner.CheckRealm(t)
}

func TestV8RealmOwner(t *testing.T) {
	v8Runner.CheckRealmOwner(t)
}

func TestV8RealmShared(t *testing.T) {
	v8Runner.CheckRealmShared(t)
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
