// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

// FunctionCaller returns the value of the caller property of the non-strict ordinary function f:
// the function that made the innermost running call of f, or null. It throws a TypeError when f
// is not such a function.
func (cx *ExecutionContext) FunctionCaller(f Value) Value {
	i := cx.innermostCall(f)
	if i < 0 {
		return Null
	}
	return cx.callerOf(i)
}

// FunctionArguments returns the value of the arguments property of the non-strict ordinary
// function f: a new arguments object of the innermost running call of f, or null. It throws a
// TypeError when f is not such a function.
func (cx *ExecutionContext) FunctionArguments(f Value) Value {
	i := cx.innermostCall(f)
	if i < 0 {
		return Null
	}
	return Value{o: cx.callArguments(i)}
}

// innermostCall returns the index of the frame of the innermost running call of the non-strict
// ordinary function f, or -1 when f is not running. It throws a TypeError when f is not such a
// function.
func (cx *ExecutionContext) innermostCall(f Value) int {
	o := f.o
	if o == nil || o.fn == nil || o.fn.code == nil || !o.fn.code.legacyCallerArguments {
		cx.ThrowTypeError("'caller' and 'arguments' are only available on non-strict functions")
	}
	i := len(cx.frames) - 1
	for i >= 0 && cx.frames[i].cl != o {
		i--
	}
	return i
}

// callArguments returns a new arguments object with the current arguments of the call of
// frames[i], which are not mapped to its parameters.
func (cx *ExecutionContext) callArguments(i int) *Object {
	fr := &cx.frames[i]
	o := cx.newArgumentsObject(cx.frameRegs(fr)[:min(fr.argc, fr.fp.nparams)], cx.extraArgs(fr))
	cx.add(o, cx.Key(keyCallee), Value{o: fr.cl}, FlagsHidden)
	return o
}

// callerOf returns the function that made the call of frames[i], or null when the call came
// from a native function that shows a frame or from script code, or when the caller is strict, a
// generator or an async function. After a proper tail call, the caller is that of the function
// that made it.
func (ag *agent) callerOf(i int) Value {
	// Eval code runs on behalf of the code that calls eval.
	j := i - 1
	for j >= 0 && ag.frames[j].fp.IsEval {
		j--
	}
	if j < 0 {
		return Null
	}
	// A native function called by the frame j or an eval frame above it made the call, unless
	// it shows no frame.
	for k := len(ag.natives) - 1; k >= 0 && ag.natives[k].frames > j; k-- {
		if c := &ag.natives[k]; c.frames <= i && c.showsFrame() {
			return Null
		}
	}
	fp := ag.frames[j].fp
	if fp.IsScript || fp.strict || fp.isGenerator || fp.isAsync {
		return Null
	}
	return Value{o: ag.frames[j].cl}
}
