// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma_test

import (
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"

	"github.com/hajimehoshi/ekuma"
	"github.com/hajimehoshi/ekuma/internal/icutest"
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

func TestICU(t *testing.T) {
	icutest.Run(t)
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

func TestExpectationDirectories(t *testing.T) {
	jstest.CheckExpectationDirectories(t)
}

func TestTest262ExpectedFailureWithMissingInclude(t *testing.T) {
	test262Runner.CheckExpectedFailureWithMissingInclude(t)
}

func TestTest262EarlyNegativeWithRuntimeError(t *testing.T) {
	test262Runner.CheckEarlyNegativeWithRuntimeError(t)
}

func TestTest262ModuleJobs(t *testing.T) {
	synctest.Test(t, test262Runner.CheckModuleJobs)
}

func TestTest262MetadataDelimiters(t *testing.T) {
	dir := t.TempDir()
	for name, src := range map[string]string{
		"overlap.js": "/*---*/",
		"stray-close.js": `// ---*/
/*---
flags: [module]
---*/
export default 1;`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	test262Runner.RunScripts(t, dir)
}

func TestTest262ModuleRealms(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"marker_FIXTURE.js": "export const marker = globalThis.marker;",
		"marker.js": `/*---
flags: [module]
---*/
globalThis.marker = "outer";
assert.sameValue((await import("./marker_FIXTURE.js")).marker, "outer");
const shadow = new ShadowRealm();
shadow.evaluate('globalThis.marker = "inner"');
assert.sameValue(await shadow.importValue("./marker_FIXTURE.js", "marker"), "inner");
shadow.evaluate('globalThis.marker = "changed"');
assert.sameValue(await shadow.importValue("././marker_FIXTURE.js", "marker"), "inner");`,
		"data_FIXTURE.json": "[42]",
		"typed.js": `/*---
flags: [module]
---*/
const other = $262.createRealm();
const load = other.evalScript('(type => import("./data_FIXTURE.json", { with: { type } }))');
assert.sameValue(Object.getPrototypeOf((await load("json")).default), other.global.Array.prototype);
assert.sameValue(Object.getPrototypeOf((await load("bytes")).default), other.global.Uint8Array.prototype);
assert.sameValue((await load("text")).default, "[42]");`,
		"source.js": `/*---
flags: [module]
---*/
import source initial from "<module source>";
const other = $262.createRealm();
const src = await other.evalScript('import.source("<module source>")');
assert.sameValue(src === initial, false);
assert.sameValue(src instanceof other.global.$262.AbstractModuleSource, true);
assert.sameValue(await other.evalScript('import.source("<module source>")'), src);`,
	}
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	test262Runner.RunScripts(t, dir)
}

func TestV8(t *testing.T) {
	v8Runner.Run(t)
}

func TestV8Test262(t *testing.T) {
	test262Runner.RunV8LocalTests(t)
}

func TestV8Throws(t *testing.T) {
	v8Runner.CheckThrows(t)
}

func TestV8ExposeGC(t *testing.T) {
	v8Runner.CheckExposeGC(t)
}

func TestV8QueueMicrotask(t *testing.T) {
	v8Runner.CheckQueueMicrotask(t)
}

func TestV8FixedTime(t *testing.T) {
	v8Runner.CheckFixedTime(t)
}

func TestV8FilesOrder(t *testing.T) {
	v8Runner.CheckFilesOrder(t)
}

func TestV8WebKit(t *testing.T) {
	v8Runner.CheckWebKit(t)
}

func TestV8Mozilla(t *testing.T) {
	v8Runner.CheckMozilla(t)
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

func TestV8Print(t *testing.T) {
	v8Runner.CheckPrint(t)
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
