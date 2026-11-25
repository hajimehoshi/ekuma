// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"cmp"
	"debug/dwarf"
	"debug/elf"
	"debug/macho"
	"debug/pe"
	_ "embed"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/engine"
)

//go:embed interp.go
var interpSource string

// fastPathCalls are the calls that the fast paths of the interpreter loop may make: builtins and
// conversions that compile to plain instructions, and helpers that the compiler inlines there.
var fastPathCalls = map[string]bool{
	"len":                   true,
	"cap":                   true,
	"min":                   true,
	"max":                   true,
	"byte":                  true,
	"float32":               true,
	"float64":               true,
	"int":                   true,
	"int8":                  true,
	"int16":                 true,
	"int32":                 true,
	"int64":                 true,
	"uint":                  true,
	"uint16":                true,
	"uint32":                true,
	"uint64":                true,
	"storage.Number":        true,
	"storage.Bool":          true,
	"site.Entry":            true,
	"this.IsObject":         true,
	"clearFrameRegs":        true,
	"storage.SetNumber":     true,
	"numconv.InInt64Range":  true,
	"ag.InterruptFlag.Load": true,
	"t.Type.Size":           true,
	"math.Float32frombits":  true,
	"math.Float32bits":      true,
	"math.Float64frombits":  true,
	"math.Float64bits":      true,
}

// fastPathMethods are the methods that the fast paths may call on a value of any name.
var fastPathMethods = map[string]bool{
	"IsNullish":    true,
	"FirstCleared": true,
	"IsHTMLDDA":    true,
}

// TestExecFastPathsCallNothing tests that the head of the interpreter loop and its first switch
// call no function and allocate nothing. Any call there makes the compiler keep the values that
// the loop carries on the stack for every instruction.
func TestExecFastPathsCallNothing(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "interp.go", interpSource, 0)
	if err != nil {
		t.Fatal(err)
	}
	fast := fastPath(f)
	if fast == nil {
		t.Fatal("no switch on i.Op at the start of the loop of (*agent).exec")
	}
	t.Run("source", func(t *testing.T) {
		for _, s := range fast {
			testFastPathSource(t, fset, s)
		}
	})
	t.Run("compiled", func(t *testing.T) {
		first := fset.Position(fast[0].Pos()).Line
		last := fset.Position(fast[len(fast)-1].End()).Line
		testFastPathCode(t, first, last)
	})
}

// testFastPathSource tests that the fast path s calls only the functions that fastPathCalls and
// fastPathMethods list, apart from the checks of builds with the build tag ekumavalidate.
func testFastPathSource(t *testing.T, fset *token.FileSet, s ast.Stmt) {
	// An assertion to a pointer type that reports its success compiles to a comparison of the
	// type word.
	commaOK := map[*ast.TypeAssertExpr]bool{}
	ast.Inspect(s, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			if len(n.Lhs) == 2 && len(n.Rhs) == 1 {
				if a, ok := n.Rhs[0].(*ast.TypeAssertExpr); ok {
					if _, ok := a.Type.(*ast.StarExpr); ok {
						commaOK[a] = true
					}
				}
			}
		case *ast.TypeAssertExpr:
			if !commaOK[n] {
				t.Errorf("%v: %T", fset.Position(n.Pos()), n)
			}
		case *ast.IfStmt:
			// validateExec is false in the build that the compiled code is tested in.
			if id, ok := n.Cond.(*ast.Ident); ok && id.Name == "validateExec" && n.Else == nil {
				return false
			}
		case *ast.CallExpr:
			name := types.ExprString(n.Fun)
			if sel, ok := n.Fun.(*ast.SelectorExpr); ok && fastPathMethods[sel.Sel.Name] {
				return true
			}
			if !fastPathCalls[name] {
				t.Errorf("%v: call of %s", fset.Position(n.Pos()), name)
			}
		case *ast.FuncLit, *ast.GoStmt, *ast.DeferStmt, *ast.SendStmt, *ast.SelectStmt:
			t.Errorf("%v: %T", fset.Position(n.Pos()), n)
		case *ast.CompositeLit:
			if _, ok := n.Type.(*ast.Ident); !ok && types.ExprString(n.Type) != "storage.Value" {
				t.Errorf("%v: literal of %s", fset.Position(n.Pos()), types.ExprString(n.Type))
			}
		case *ast.UnaryExpr:
			if n.Op == token.ARROW {
				t.Errorf("%v: channel receive", fset.Position(n.Pos()))
			}
			if _, ok := n.X.(*ast.CompositeLit); ok && n.Op == token.AND {
				t.Errorf("%v: allocation", fset.Position(n.Pos()))
			}
		case *ast.BinaryExpr:
			for _, x := range []ast.Expr{n.X, n.Y} {
				if l, ok := x.(*ast.BasicLit); ok && l.Kind == token.STRING {
					t.Errorf("%v: string concatenation", fset.Position(n.Pos()))
				}
			}
		}
		return true
	})
}

// testFastPathCode tests that the machine code of the lines first to last of interp.go in
// (*agent).exec calls nothing but write barriers and panics, in a test binary of this package built
// with the default settings for the architecture that runs the test.
func testFastPathCode(t *testing.T, first, last int) {
	// On 386, the fast paths call runtime functions that convert between floats and integers, and
	// on 386 and wasm their interrupt check is a call.
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		t.Skipf("the fast paths are not call-free on %s", runtime.GOARCH)
	}
	gocmd, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("no go command: %v", err)
	}

	// The binary of a running test has no symbol table, so build one that has.
	bin := filepath.Join(t.TempDir(), "engine.test")
	build := exec.Command(gocmd, "test", "-c", "-o", bin, ".")
	build.Env = append(os.Environ(), "GOFLAGS=", "GOOS="+runtime.GOOS, "GOARCH="+runtime.GOARCH)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go test -c: %v\n%s", err, out)
	}

	execName := reflect.TypeFor[engine.ExecutionContext]().PkgPath() + ".(*agent).exec"
	objdump := exec.Command(gocmd, "tool", "objdump", "-s", "^"+regexp.QuoteMeta(execName)+"$", bin)
	// The tool is for the host, whatever the target of the test is.
	objdump.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool {
		k, _, _ := strings.Cut(kv, "=")
		return k == "GOOS" || k == "GOARCH" || k == "GOFLAGS"
	})
	var stderr strings.Builder
	objdump.Stderr = &stderr
	out, err := objdump.Output()
	if err != nil {
		t.Fatalf("go tool objdump: %v\n%s", err, stderr.String())
	}
	code := parseObjdump(string(out))
	if len(code) == 0 {
		t.Fatalf("go tool objdump listed no code of %s", execName)
	}

	d, err := openDWARF(bin)
	if err != nil {
		t.Fatal(err)
	}
	inlined, entry, err := inlinedCalls(d, execName)
	if err != nil {
		t.Fatal(err)
	}
	if entry != code[0].addr {
		t.Fatalf("%s starts at %#x in the debugging information and at %#x in go tool objdump", execName, entry, code[0].addr)
	}
	if len(inlined) == 0 {
		t.Fatalf("the debugging information has no function inlined into %s", execName)
	}

	var lines, calls int
	reported := map[string]bool{}
	for _, in := range code {
		if in.op == "CALL" {
			calls++
		}
		line := execLine(inlined, in)
		if line < first || line > last {
			continue
		}
		lines++
		if in.op != "CALL" || keepsRegisters(in.args) {
			continue
		}
		msg := fmt.Sprintf("interp.go:%d: call of %s", line, in.args)
		if in.pos != "interp.go:"+strconv.Itoa(line) {
			msg += " at " + in.pos
		}
		if !reported[msg] {
			reported[msg] = true
			t.Error(msg)
		}
	}
	if calls == 0 {
		t.Fatalf("go tool objdump listed no call in %s", execName)
	}
	if lines == 0 {
		t.Fatalf("no instruction of %s is at lines %d to %d of interp.go", execName, first, last)
	}
}

// keepsRegisters reports whether a call of target leaves the values in registers: a write barrier
// saves the registers that it uses, and a panic does not return.
func keepsRegisters(target string) bool {
	return strings.HasPrefix(target, "runtime.gcWriteBarrier") || strings.HasPrefix(target, "runtime.panic")
}

// objdumpInstruction is an instruction that go tool objdump lists.
type objdumpInstruction struct {
	addr uint64
	// pos is the file and line of the instruction in the innermost inlined function.
	pos  string
	op   string
	args string
}

// parseObjdump returns the instructions in out, an output of go tool objdump.
func parseObjdump(out string) []objdumpInstruction {
	var code []objdumpInstruction
	for line := range strings.Lines(out) {
		// The position, the address, the encoding, the operation and its arguments.
		fs := strings.Fields(line)
		if len(fs) < 4 || fs[0] == "TEXT" {
			continue
		}
		addr, err := strconv.ParseUint(strings.TrimPrefix(fs[1], "0x"), 16, 64)
		if err != nil {
			continue
		}
		code = append(code, objdumpInstruction{
			addr: addr,
			pos:  fs[0],
			op:   fs[3],
			args: strings.Join(fs[4:], " "),
		})
	}
	return code
}

// openDWARF returns the debugging information of the executable file at path.
func openDWARF(path string) (*dwarf.Data, error) {
	switch runtime.GOOS {
	case "darwin", "ios":
		f, err := macho.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return f.DWARF()
	case "windows":
		f, err := pe.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return f.DWARF()
	default:
		f, err := elf.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return f.DWARF()
	}
}

// inlinedCall is a range of addresses that the code of an inlined function takes up.
type inlinedCall struct {
	lo, hi uint64
	// line is the line of the call in the function that the code is inlined into.
	line int
}

// inlinedCalls returns the address ranges of the outermost functions inlined into the function
// fn, sorted by address, and the entry address of fn.
func inlinedCalls(d *dwarf.Data, fn string) ([]inlinedCall, uint64, error) {
	r := d.Reader()
	for {
		e, err := r.Next()
		if err != nil {
			return nil, 0, err
		}
		if e == nil {
			return nil, 0, fmt.Errorf("no debugging information for %s", fn)
		}
		if e.Tag == dwarf.TagCompileUnit {
			continue
		}
		if e.Tag != dwarf.TagSubprogram || e.Val(dwarf.AttrName) != fn || !e.Children {
			r.SkipChildren()
			continue
		}
		entry, _ := e.Val(dwarf.AttrLowpc).(uint64)
		var calls []inlinedCall
		for depth := 1; depth > 0; {
			e, err := r.Next()
			if err != nil {
				return nil, 0, err
			}
			if e == nil {
				break
			}
			switch {
			case e.Tag == 0:
				depth--
			case e.Tag == dwarf.TagInlinedSubroutine:
				line, _ := e.Val(dwarf.AttrCallLine).(int64)
				ranges, err := d.Ranges(e)
				if err != nil {
					return nil, 0, err
				}
				for _, rg := range ranges {
					calls = append(calls, inlinedCall{
						lo:   rg[0],
						hi:   rg[1],
						line: int(line),
					})
				}
				// Functions inlined into this one are at the same line of fn.
				r.SkipChildren()
			case e.Children:
				depth++
			}
		}
		slices.SortFunc(calls, func(a, b inlinedCall) int {
			return cmp.Compare(a.lo, b.lo)
		})
		return calls, entry, nil
	}
}

// execLine returns the line of interp.go that the instruction in belongs to, outside every
// function inlined into its function, or 0. inlined is the result of inlinedCalls for its
// function.
func execLine(inlined []inlinedCall, in objdumpInstruction) int {
	i, _ := slices.BinarySearchFunc(inlined, in.addr, func(c inlinedCall, addr uint64) int {
		return cmp.Compare(c.lo, addr+1)
	})
	if i > 0 && in.addr < inlined[i-1].hi {
		return inlined[i-1].line
	}
	file, line, ok := strings.Cut(in.pos, ":")
	if !ok || file != "interp.go" {
		return 0
	}
	n, _ := strconv.Atoi(line)
	return n
}

// fastPath returns the statements of the loop of (*agent).exec up to its first switch on i.Op, or
// nil.
func fastPath(f *ast.File) []ast.Stmt {
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "exec" || fn.Recv == nil {
			continue
		}
		for _, s := range fn.Body.List {
			loop, ok := s.(*ast.ForStmt)
			if !ok {
				continue
			}
			for i, s := range loop.Body.List {
				if sw, ok := s.(*ast.SwitchStmt); ok && types.ExprString(sw.Tag) == "i.Op" {
					return loop.Body.List[:i+1]
				}
			}
		}
	}
	return nil
}
