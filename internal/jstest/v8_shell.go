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

type v8Shell struct {
	stdout io.Writer
	stderr io.Writer
	origin time.Time
	timers map[string]time.Time
	marks  map[string]float64
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

func (s *v8Shell) install(ctx context.Context, r *ekuma.Realm, checkout fs.FS, canAccessReceiver func(*ekuma.Realm, ekuma.Value) bool) error {
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
	checkReceiver := r.FuncOf("canAccessReceiver", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		return canAccessReceiver(r, args[0]), nil
	})
	now := r.FuncOf("now", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		return s.timestamp(), nil
	})
	_, err = init.Invoke(ctx, map[string]any{
		"readFile":          r.FuncOf("readFile", readFile),
		"stdout":            r.FuncOf("stdout", write(s.stdout)),
		"stderr":            r.FuncOf("stderr", write(s.stderr)),
		"timer":             r.FuncOf("timer", s.timer),
		"exit":              exit,
		"canAccessReceiver": checkReceiver,
		"now":               now,
		"recordMark":        r.FuncOf("recordMark", s.mark),
		"lookupMark":        r.FuncOf("lookupMark", s.lookupMark),
		"osName":            v8OSName(),
		"d8Path":            os.Args[0],
	})
	return err
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
	if err := shell.install(t.Context(), vm.InitialRealm(), nil, host.CanAccessReceiver); err != nil {
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
