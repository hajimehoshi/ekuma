// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package jstest

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/hajimehoshi/ekuma"
)

//go:embed testdata/harness/v8-shell.js
var v8ShellSource string

var v8ShellScript = sync.OnceValues(func() (*ekuma.UnboundScript, error) {
	return ekuma.ParseUnboundScript("v8-shell.js", v8ShellSource)
})

// A host panic carries the exit code past JavaScript catch and finally blocks.
type v8ExitCode int32

var errV8Termination = errors.New("d8 execution terminated")

type v8Shell struct {
	stdout   io.Writer
	stderr   io.Writer
	origin   time.Time
	timers   map[string]time.Time
	marks    map[string]float64
	domNodes ekuma.Value

	recordPrint       ekuma.Func
	withdrawInterrupt func()
}

func newV8Shell(stdout, stderr io.Writer) v8Shell {
	return v8Shell{
		stdout: stdout,
		stderr: stderr,
		origin: time.Now(),
		timers: make(map[string]time.Time),
		marks:  make(map[string]float64),
	}
}

func (s *v8Shell) close() {
	if s.withdrawInterrupt != nil {
		s.withdrawInterrupt()
		s.withdrawInterrupt = nil
	}
}

func (s *v8Shell) install(ctx context.Context, host *V8, r *ekuma.Realm, scripts *v8Scripts) error {
	if err := scripts.installLoader(ctx, r); err != nil {
		return err
	}
	script, err := v8ShellScript()
	if err != nil {
		return err
	}
	init, err := r.BindScript(script).Evaluate(ctx)
	if err != nil {
		return err
	}
	readFile := func(this ekuma.Value, args []ekuma.Value) (any, error) {
		name := args[0].String()
		filename, _, _ := strings.Cut(name, "\x00")
		data, err := fs.ReadFile(scripts.checkout, path.Clean(filepath.ToSlash(filename)))
		if err != nil {
			if len(name) > 50 {
				name = name[:50] + "[...]"
			}
			return `Error reading file "` + name + `"`, nil
		}
		// Allocate a JavaScript-owned buffer so the caller can transfer it.
		view, err := args[1].New(ctx, len(data))
		if err != nil {
			return nil, err
		}
		ekuma.CopyBytesToJS(view, data)
		return view, nil
	}
	write := func(w io.Writer) ekuma.Func {
		return func(this ekuma.Value, args []ekuma.Value) (any, error) {
			_, err := io.WriteString(w, args[0].String())
			return ekuma.Undefined, err
		}
	}
	var recordPrint ekuma.Value
	if s.recordPrint != nil {
		recordPrint = r.FuncOf("recordPrint", s.recordPrint)
	}
	exit := r.FuncOf("exit", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		panic(v8ExitCode(args[0].Int()))
	})
	terminate := r.FuncOf("terminate", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		if s.withdrawInterrupt == nil {
			s.withdrawInterrupt = host.RequestInterrupt(r, errV8Termination)
		}
		return ekuma.Undefined, nil
	})
	terminateNow := r.FuncOf("terminateNow", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		host.InterruptNow(r, errV8Termination)
		return ekuma.Undefined, nil
	})
	checkReceiver := r.FuncOf("canAccessReceiver", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		return host.CanAccessReceiver(r, args[0]), nil
	})
	now := r.FuncOf("now", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		return s.timestamp(), nil
	})
	installed, err := init.Invoke(ctx, map[string]any{
		"readFile":          r.FuncOf("readFile", readFile),
		"stdout":            r.FuncOf("stdout", write(s.stdout)),
		"stderr":            r.FuncOf("stderr", write(s.stderr)),
		"recordPrint":       recordPrint,
		"printLimit":        maxDiagnosticLineBytes,
		"timer":             r.FuncOf("timer", s.timer),
		"exit":              exit,
		"terminate":         terminate,
		"terminateNow":      terminateNow,
		"canAccessReceiver": checkReceiver,
		"now":               now,
		"recordMark":        r.FuncOf("recordMark", s.mark),
		"lookupMark":        r.FuncOf("lookupMark", s.lookupMark),
		"osName":            v8OSName(),
		"d8Path":            os.Args[0],
		"domNodes":          s.domNodes,
	})
	if err != nil {
		return err
	}
	nodes, err := installed.Get(ctx, "domNodes")
	if err != nil {
		return err
	}
	report, err := installed.Get(ctx, "reportException")
	if err != nil {
		return err
	}
	s.domNodes = nodes
	host.SetExceptionReporter(r, report)
	return nil
}

func v8OSName() string {
	switch runtime.GOOS {
	case "darwin":
		return "macos"
	case "android", "ios", "linux", "windows":
		return runtime.GOOS
	default:
		return "unknown"
	}
}

func (s *v8Shell) timestamp() float64 {
	return float64(time.Since(s.origin)) / float64(time.Millisecond)
}

// d8 keys its per-isolate mark table by NUL-terminated UTF-8 names.
func v8MarkKey(name ekuma.Value) string {
	key, _, _ := strings.Cut(name.String(), "\x00")
	return key
}

func (s *v8Shell) mark(this ekuma.Value, args []ekuma.Value) (any, error) {
	timestamp := s.timestamp()
	// A stored key can outlive the full name, including any suffix after NUL.
	s.marks[strings.Clone(v8MarkKey(args[0]))] = timestamp
	return timestamp, nil
}

func (s *v8Shell) lookupMark(this ekuma.Value, args []ekuma.Value) (any, error) {
	key := v8MarkKey(args[0])
	timestamp, ok := s.marks[key]
	if !ok {
		const maxMessage = 1024
		message := `Invalid performance.mark "` + key[:min(len(key), maxMessage)] + `" does not exist`
		return nil, errors.New(message[:min(len(message), maxMessage)])
	}
	return timestamp, nil
}

func (s *v8Shell) timer(this ekuma.Value, args []ekuma.Value) (any, error) {
	method, label := args[0].String(), args[1].String()
	start, exists := s.timers[label]
	// d8 keys timers by the full label and prints them as C strings.
	display, _, _ := strings.Cut(label, "\x00")
	var err error
	switch {
	case method == "time" && !exists:
		s.timers[label] = time.Now()
	case method == "time":
		_, err = fmt.Fprintf(s.stdout, "console.time: Timer '%s' already exists\n", display)
	case method != "timeStamp" && !exists:
		_, err = fmt.Fprintf(s.stdout, "console.%s: Timer '%s' does not exist\n", method, display)
	default:
		if method == "timeStamp" {
			start = s.origin
		}
		_, err = fmt.Fprintf(s.stdout, "console.%s: %s, %f\n", method, display, float64(time.Since(start))/float64(time.Millisecond))
		if method == "timeEnd" {
			delete(s.timers, label)
		}
	}
	return ekuma.Undefined, err
}

// CheckShellOutput checks output streams and partial writes when coercion throws.
func (host *V8) CheckShellOutput(t *testing.T) {
	var vm ekuma.VM
	var stdout, stderr strings.Builder
	shell := newV8Shell(&stdout, &stderr)
	defer shell.close()
	scripts := newV8Scripts(host, vm.InitialRealm(), nil, ".")
	if err := shell.install(t.Context(), host, scripts.current, scripts); err != nil {
		t.Fatal(err)
	}
	const source = `
		console.warn("item", Symbol("label"));
		const token = {};
		try {
			printErr("partial", { toString() { throw token; } });
			throw Error("expected printErr to throw");
		} catch (e) { if (e !== token) throw e; }
		console.error("done");
	`
	if _, err := runScript(t.Context(), &vm, "output.js", source); err != nil {
		t.Error(err)
	}
	if got, want := stdout.String(), "console.warn: item label\n"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
	if got, want := stderr.String(), "partial \nconsole.error: done\n"; got != want {
		t.Errorf("stderr = %q, want %q", got, want)
	}
}

// CheckPrint checks mjsunit argument conversion and the last-print diagnostic.
func (host *V8) CheckPrint(t *testing.T) {
	const source = `
		const child = Realm.create();
		let order = "";
		function argument(id, result) {
			return { [Symbol.toPrimitive](hint) {
				if (hint !== "string") throw Error(hint);
				order += id;
				return result;
			} };
		}
		Realm.shared = [argument(1, "x".repeat(250)), argument(2, {}), argument(3, "")];
		try {
			Realm.eval(child, "print(...Realm.shared)");
			throw Error("expected print to throw");
		} catch (e) {
			if (e.constructor !== Realm.eval(child, "TypeError")) throw e;
		}
		if (order !== "12") throw Error("conversion order: " + order);
		console.log("after print");
	`
	test := v8Test{
		file: "test/mjsunit/print.js",
	}
	checkout := fstest.MapFS{
		test.file: {
			Data: []byte(source),
		},
	}
	result, printed := test.execute(t.Context(), host, checkout, testTimeout)
	if result.outcome != suitePassed {
		t.Error(result.reason)
	}
	if want := "[" + strings.Repeat("x", maxDiagnosticLineBytes-1); printed != want {
		t.Errorf("last print = %q, want %q", printed, want)
	}
}

// CheckQuit checks that quitting from a helper ends the test with the requested status.
func (host *V8) CheckQuit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		helper string
		want   suiteOutcome
	}{
		{
			name: "nonzero exit from a child realm",
			helper: `
				Promise.resolve().then(() => print("ran a queued job"));
				try {
					Realm.eval(Realm.create(), 'd8.quit("1.5")');
				} catch {
					print("caught quit");
				} finally {
					print("ran finally");
				}`,
			want: suiteFailed,
		},
		{
			name:   "exit code conversion throws",
			helper: `quit({valueOf() { throw new Error(); }});`,
			want:   suitePassed,
		},
		{
			name:   "timer quits after a script error",
			helper: `setTimeout(() => quit()); throw new Error("before the timer");`,
			want:   suitePassed,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			test := v8Test{
				file: "test/mjsunit/quit.js",
			}
			checkout := fstest.MapFS{
				"helper.js": {
					Data: []byte(tc.helper),
				},
				test.file: {
					Data: []byte("// Files: helper.js\nprint('ran the test body');"),
				},
			}
			result, lastPrint := test.execute(t.Context(), host, checkout, testTimeout)
			if result.outcome != tc.want {
				t.Errorf("outcome = %v, want %v (%s)", result.outcome, tc.want, result.reason)
			}
			if lastPrint != "" {
				t.Errorf("execution continued after quit: %s", lastPrint)
			}
		})
	}
}

// CheckTermination checks how an interrupt ends execution of one mjsunit case.
func (host *V8) CheckTermination(t *testing.T) {
	test := v8Test{
		file: "test/mjsunit/terminate.js",
	}
	t.Run("deadline during termination", func(t *testing.T) {
		ctx, cancel := context.WithCancelCause(t.Context())
		defer cancel(nil)
		h := *host
		h.RequestInterrupt = func(r *ekuma.Realm, cause error) func() {
			withdraw := host.RequestInterrupt(r, cause)
			cancel(context.DeadlineExceeded)
			return withdraw
		}
		checkout := fstest.MapFS{
			test.file: {
				Data: []byte("d8.terminate(); for (let i = 0; i < 100; ++i) {}"),
			},
		}
		result, _ := test.execute(ctx, &h, checkout, testTimeout)
		if result.outcome != suiteBroken || !result.retry {
			t.Errorf("result = %+v, want a retryable timeout", result)
		}
	})
	for _, tc := range []struct {
		name      string
		script    string
		want      suiteOutcome
		wantPrint string
	}{
		{
			name: "exception reporting before jobs",
			script: `
				Promise.resolve().then(() => print("ran a job"));
				onerror = () => {
					try {
						d8.terminate(7);
						for (let i = 0; i < 100000; ++i) {}
					} finally { print("ran finally"); }
				};
				throw 1;`,
			want: suitePassed,
		},
		{
			name: "interrupt observed after script completion",
			script: `
				Promise.resolve().then(() => print("ran a job"));
				d8.terminate();`,
			want: suiteFailed,
		},
		{
			name: "immediate termination at script end",
			script: `
				Promise.resolve().then(() => print("ran a job"));
				d8.terminateNow();`,
			want: suitePassed,
		},
		{
			name: "immediate termination during dynamic import checkpoint",
			script: `
				function stop() {
					setTimeout(() => print("ran a timer"));
					Promise.resolve().then(() => print("ran a job"));
					print("entered module");
					try {
						d8.terminateNow();
						print("returned");
					} catch { print("caught"); }
					finally { print("ran finally"); }
				}
				import("data:text/javascript,stop()");`,
			want:      suiteFailed,
			wantPrint: "[entered module]",
		},
		{
			name: "timer preserves earlier failure",
			script: `
				setTimeout(() => { throw 1; });
				setTimeout(() => {
					d8.terminate();
					for (let i = 0; i < 100000; ++i) {}
				});
				setTimeout(() => print("ran a timer"));`,
			want: suiteFailed,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkout := fstest.MapFS{
				"helper.js": {
					Data: []byte(tc.script),
				},
				test.file: {
					Data: []byte("// Files: helper.js\nsetTimeout(() => print('ran the test timer'));"),
				},
			}
			result, printed := test.execute(t.Context(), host, checkout, testTimeout)
			if result.outcome != tc.want {
				t.Errorf("outcome = %v, want %v (%s)", result.outcome, tc.want, result.reason)
			}
			if printed != tc.wantPrint {
				t.Errorf("last print = %q, want %q", printed, tc.wantPrint)
			}
		})
	}
}
