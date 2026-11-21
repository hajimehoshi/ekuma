// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package jstest

import (
	"context"
	_ "embed"
	"fmt"
	"io"
	"io/fs"
	"path"
	"path/filepath"
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

type v8Shell struct {
	stdout io.Writer
	stderr io.Writer
	origin time.Time
	timers map[string]time.Time
}

func newV8Shell(stdout, stderr io.Writer) v8Shell {
	return v8Shell{
		stdout: stdout,
		stderr: stderr,
		origin: time.Now(),
		timers: make(map[string]time.Time),
	}
}

func (s *v8Shell) install(ctx context.Context, r *ekuma.Realm, checkout fs.FS) error {
	if err := installV8ScriptLoader(ctx, r, checkout); err != nil {
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
		data, err := fs.ReadFile(checkout, path.Clean(filepath.ToSlash(filename)))
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
	exit := r.FuncOf("exit", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		panic(v8ExitCode(args[0].Int()))
	})
	_, err = init.Invoke(ctx, r.FuncOf("readFile", readFile), r.FuncOf("stdout", write(s.stdout)),
		r.FuncOf("stderr", write(s.stderr)), r.FuncOf("timer", s.timer), exit)
	return err
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

// CheckV8ShellOutput checks output streams and partial writes when coercion throws.
func CheckV8ShellOutput(t *testing.T) {
	var vm ekuma.VM
	var stdout, stderr strings.Builder
	shell := newV8Shell(&stdout, &stderr)
	if err := shell.install(t.Context(), vm.InitialRealm(), nil); err != nil {
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
