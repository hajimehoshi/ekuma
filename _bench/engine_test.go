// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package bench_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/MoeForge/moejs"
	"github.com/dop251/goja"
	"github.com/fastschema/qjs"
	"github.com/grafana/sobek"
	"github.com/robertkrimen/otto"
	"github.com/tommie/v8go"
	"modernc.org/quickjs"

	"github.com/hajimehoshi/ekuma"
	"github.com/hajimehoshi/ekuma/_bench/internal/comparison"
)

// A vm is an instance of an engine.
type vm interface {
	// eval evaluates the script src, named name, in the global scope.
	eval(name, src string) error

	close()
}

// errV8Mode is the error of creating a v8go instance in a process whose V8 runs in the other
// mode: the flags of V8 are fixed when its first isolate is created.
var errV8Mode = errors.New("V8 already runs in the other mode in this process; run v8go and v8go-jitless in separate processes")

// newVM creates an instance of the named engine.
func newVM(engine string) (vm, error) {
	switch engine {
	case comparison.Baseline:
		return &ekumaVM{}, nil
	case "goja":
		return &gojaVM{
			vm: goja.New(),
		}, nil
	case "sobek":
		return &sobekVM{
			vm: sobek.New(),
		}, nil
	case "moejs":
		return &moejsVM{
			rt: moejs.NewRuntime(moejs.Options{
				MutableIntrinsics: true,
			}),
		}, nil
	case "otto":
		return &ottoVM{
			vm: otto.New(),
		}, nil
	case "quickjs":
		m, err := quickjs.NewVM()
		if err != nil {
			return nil, err
		}
		return &quickjsVM{
			vm: m,
		}, nil
	case "qjs":
		rt, err := qjs.New()
		if err != nil {
			return nil, err
		}
		return &qjsVM{
			rt: rt,
		}, nil
	case "quickjs-c":
		return newQuickJSCVM()
	case "v8go-jitless":
		return newV8VM("--jitless")
	case "v8go":
		return newV8VM("")
	}
	return nil, fmt.Errorf("unknown engine %q", engine)
}

type ekumaVM struct {
	vm ekuma.VM
}

func (v *ekumaVM) eval(name, src string) error {
	s, err := v.vm.ParseScript(name, src)
	if err != nil {
		return err
	}
	_, err = s.Evaluate(context.Background())
	return err
}

func (v *ekumaVM) close() {}

type gojaVM struct {
	vm *goja.Runtime
}

func (v *gojaVM) eval(name, src string) error {
	_, err := v.vm.RunScript(name, src)
	return err
}

func (v *gojaVM) close() {}

type sobekVM struct {
	vm *sobek.Runtime
}

func (v *sobekVM) eval(name, src string) error {
	_, err := v.vm.RunScript(name, src)
	return err
}

func (v *sobekVM) close() {}

type moejsVM struct {
	rt *moejs.Runtime
}

func (v *moejsVM) eval(name, src string) error {
	s, err := moejs.CompileScript(name, src)
	if err != nil {
		return err
	}
	_, err = v.rt.RunScript(s)
	return err
}

func (v *moejsVM) close() {}

type ottoVM struct {
	vm *otto.Otto
}

func (v *ottoVM) eval(name, src string) error {
	s, err := v.vm.Compile(name, src)
	if err != nil {
		return err
	}
	_, err = v.vm.Run(s)
	return err
}

func (v *ottoVM) close() {}

type quickjsVM struct {
	vm *quickjs.VM
}

func (v *quickjsVM) eval(name, src string) error {
	_, err := v.vm.Eval(src, quickjs.EvalGlobal)
	return err
}

func (v *quickjsVM) close() {
	v.vm.Close()
}

type qjsVM struct {
	rt *qjs.Runtime
}

func (v *qjsVM) eval(name, src string) error {
	r, err := v.rt.Eval(name, qjs.Code(src))
	if err != nil {
		return err
	}
	r.Free()
	return nil
}

func (v *qjsVM) close() {
	v.rt.Close()
}

// quickJSCDriver is the script that the qjs process of QuickJS in C runs. It evaluates the
// scripts that it reads from its standard input, each a line with the length of the script in
// bytes followed by the script. For each, it writes a line to the file descriptor 3: "ok", or
// "error" and the error as a JSON string. Output of the scripts cannot corrupt the replies.
const quickJSCDriver = `
var replies = std.fdopen(3, "w");
for (;;) {
	var line = std.in.getline();
	if (line === null) {
		break;
	}
	var src = std.in.readAsString(+line);
	var reply = "ok";
	try {
		std.evalScript(src);
	} catch (e) {
		reply = "error " + JSON.stringify(String(e) + (e && e.stack ? "\n" + e.stack : ""));
	}
	replies.puts(reply + "\n");
	replies.flush();
}
`

// quickJSCVM is an instance of QuickJS in C: a qjs process that runs quickJSCDriver.
type quickJSCVM struct {
	cmd     *exec.Cmd
	scripts io.WriteCloser
	replies *os.File
	reader  *bufio.Reader
}

func newQuickJSCVM() (vm, error) {
	root, err := comparison.Root()
	if err != nil {
		return nil, err
	}
	bin := comparison.QuickJSPath(root)
	if _, err := os.Stat(bin); err != nil {
		return nil, fmt.Errorf("QuickJS in C is not built; run go run ./cmd/jsbench -setup: %w", err)
	}
	replies, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(bin, "--std", "-e", quickJSCDriver)
	cmd.ExtraFiles = []*os.File{w}
	cmd.Stderr = os.Stderr
	scripts, err := cmd.StdinPipe()
	if err != nil {
		replies.Close()
		w.Close()
		return nil, err
	}
	err = cmd.Start()
	// The process has its own copy of the write end.
	w.Close()
	if err != nil {
		replies.Close()
		return nil, err
	}
	return &quickJSCVM{
		cmd:     cmd,
		scripts: scripts,
		replies: replies,
		reader:  bufio.NewReader(replies),
	}, nil
}

func (v *quickJSCVM) eval(name, src string) error {
	if _, err := fmt.Fprintf(v.scripts, "%d\n%s", len(src), src); err != nil {
		return fmt.Errorf("qjs: %w", err)
	}
	reply, err := v.reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("qjs: %w", err)
	}
	reply = strings.TrimSuffix(reply, "\n")
	if reply == "ok" {
		return nil
	}
	data, ok := strings.CutPrefix(reply, "error ")
	if !ok {
		return fmt.Errorf("qjs: unexpected reply %q", reply)
	}
	var msg string
	if err := json.Unmarshal([]byte(data), &msg); err != nil {
		return fmt.Errorf("qjs: reply %q: %w", reply, err)
	}
	return fmt.Errorf("%s: %s", name, msg)
}

func (v *quickJSCVM) close() {
	// The driver ends at the end of its input.
	v.scripts.Close()
	v.cmd.Wait()
	v.replies.Close()
}

var (
	// v8Started reports whether V8 has created an isolate in this process.
	v8Started bool

	// v8Flags is the flags that V8 runs with in this process once v8Started is true.
	v8Flags string
)

type v8VM struct {
	iso *v8go.Isolate
	ctx *v8go.Context
}

// newV8VM creates a v8go instance whose V8 runs with flags.
func newV8VM(flags string) (vm, error) {
	if !v8Started {
		if flags != "" {
			v8go.SetFlags(flags)
		}
		v8Started = true
		v8Flags = flags
	} else if v8Flags != flags {
		return nil, errV8Mode
	}
	iso := v8go.NewIsolate()
	return &v8VM{
		iso: iso,
		ctx: v8go.NewContext(iso),
	}, nil
}

func (v *v8VM) eval(name, src string) error {
	_, err := v.ctx.RunScript(src, name)
	return err
}

func (v *v8VM) close() {
	v.ctx.Close()
	v.iso.Dispose()
}

// A script is a script to evaluate.
type script struct {
	name string
	src  string
}

// benchEngines runs a sub-benchmark for each engine. An instance of the engine evaluates setup
// outside the timer, and then the script that runFormat formats with b.N, which runs b.N
// iterations. The instance is kept across the calls of a sub-benchmark with a growing b.N.
func benchEngines(b *testing.B, setup []script, runFormat string) {
	for _, engine := range comparison.Engines {
		var v vm
		b.Run(engine, func(b *testing.B) {
			if v == nil {
				var err error
				if v, err = newVM(engine); err != nil {
					b.Skip(err)
				}
				for _, s := range setup {
					if err := v.eval(s.name, s.src); err != nil {
						fail(b, engine, err)
					}
				}
			}
			b.ResetTimer()
			if err := v.eval("run", fmt.Sprintf(runFormat, b.N)); err != nil {
				fail(b, engine, err)
			}
		})
		if v != nil {
			v.close()
		}
	}
}

// fail ends b for an error of engine. An error of the baseline fails the benchmark. An error of
// another engine skips the benchmark, since the comparison reports it as a missing result.
func fail(b *testing.B, engine string, err error) {
	b.Helper()
	if engine == comparison.Baseline {
		b.Fatal(err)
	}
	b.Skip(err)
}

// loadSuite reads the files of s, and skips b when they have not been fetched.
func loadSuite(b *testing.B, s *comparison.Suite) map[string]string {
	b.Helper()
	root, err := comparison.Root()
	if err != nil {
		b.Fatal(err)
	}
	srcs, err := s.Load(root)
	if errors.Is(err, fs.ErrNotExist) {
		b.Skipf("the benchmark programs are not in %s; run go run ./cmd/jsbench -setup", root)
	}
	if err != nil {
		b.Fatal(err)
	}
	return srcs
}
