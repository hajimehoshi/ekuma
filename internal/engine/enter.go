// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"crypto/subtle"

	"github.com/hajimehoshi/ekuma/internal/xerrors"
)

// hostContext is the host state in effect during an entry into the VM.
type hostContext struct {
	continuationData any

	// enteredRealm stays fixed across ordinary cross-realm calls.
	enteredRealm *Realm
}

// enterJob installs the job's host state across calls in its execution realm.
func (cx ExecutionContext) enterJob(continuationData any, f func()) {
	vm := cx.vm
	// The outer host call already entered the only realm in a single-realm VM.
	if vm.trackJobRealms {
		caller := vm.executionContext()
		entered := vm.enteredRealm
		vm.enteredRealm = cx.realm
		cx.activate()
		defer func() {
			vm.enteredRealm = entered
			caller.activate()
		}()
	}
	previous := vm.continuationData
	vm.continuationData = continuationData
	defer vm.SetContinuationData(previous)
	f()
}

// enter runs f as a call from Go and drains jobs at the outermost boundary, including
// after a JavaScript exception.
//
// An interruption or internal failure ends the outermost such call. When a host callback
// enters the engine again, the signal passes through that inner call and the Go code.
//
// A Go panic restores each boundary it escapes. The outermost boundary discards pending
// jobs and rejects unfinished module evaluations, including after a recovered inner panic.
func (vm *VM) enter(f func(ExecutionContext) Value) (result Value, err error) {
	if vm.entered > 0 {
		return vm.enterBoundary(f)
	}
	vm.enteredRealm = vm.curRealm
	vm.captureThrowSites = vm.curRealm.exceptionReporter != nil
	defer func() {
		vm.enteredRealm = nil
		vm.captureThrowSites = false
		vm.thrownAt = throwSite{}
	}()
	// Data independent timing stays enabled for the goroutine across rescheduling, so the
	// nested calls need nothing more.
	subtle.WithDataIndependentTiming(func() {
		result, err = vm.enterBoundary(f)
	})
	return result, err
}

// enterBoundary implements [VM.enter].
func (vm *VM) enterBoundary(f func(ExecutionContext) Value) (result Value, err error) {
	// A host callback that recovered an interruption runs no more JavaScript.
	vm.ResumeInterruption()
	frames, natives, sp, depth := len(vm.frames), len(vm.natives), vm.sp, vm.depth
	cx, callerRealm := vm.executionContext(), vm.callerRealm
	thrown, site := vm.thrown, vm.thrownAt
	hostContext := vm.hostContext
	outermost := vm.entered == 0
	vm.entered++
	defer func() {
		vm.entered--
		if outermost {
			defer func() {
				if vm.abortCause != nil {
					cause := vm.abortCause
					vm.abortCause = nil
					if internal, ok := cause.(*xerrors.InternalError); ok {
						result, err = Undefined, internal
					}
					vm.abortModuleEvaluations(cause)
					vm.dropJobs()
				}
				vm.raised = nil
				vm.clearKeptObjects()
				vm.clearStale()
				vm.releaseStack()
			}()
		}
		if r := recover(); r != nil {
			sig, interrupted := r.(*interruptSignal)
			internal, failed := r.(*xerrors.InternalError)
			vm.hostContext = hostContext
			// Only activations belonging to this boundary have lost their Go callers.
			cx.activate()
			for i := frames; i < len(vm.frames); i++ {
				if g := vm.frames[i].gen; g != nil {
					g.abort()
				}
			}
			vm.unwindTo(frames)
			vm.truncateNatives(natives)
			vm.sp, vm.depth = sp, depth
			vm.callerRealm = callerRealm
			vm.thrown, vm.thrownAt = thrown, site
			var cause error
			if interrupted {
				if !outermost {
					panic(r)
				}
				result, err = Undefined, &InterruptedError{
					Cause: sig.cause,
				}
				cause = sig.cause
				if cause == nil {
					cause = err
				}
			} else if failed {
				vm.abortCause = internal
				if !outermost {
					panic(r)
				}
				return
			} else {
				vm.abortCause = &PanicError{
					Value: r,
				}
				panic(r)
			}
			// The rejections of the modules queue jobs, which are dropped as well.
			vm.abortModuleEvaluations(cause)
			vm.dropJobs()
		}
	}()
	if !vm.protect(func() {
		ok := vm.protect(func() {
			result = f(cx)
		})
		vm.ResumeInterruption()
		thrown := vm.takeThrown()
		site := vm.thrownAt
		if !ok && outermost {
			vm.reportUncaughtException(thrown, site)
		}
		vm.runJobs()
		vm.ResumeInterruption()
		if !ok {
			vm.thrown, vm.thrownAt = thrown, site
			panic(errThrow)
		}
	}) {
		return Undefined, vm.takeException()
	}
	return result, nil
}

func (vm *VM) reportUncaughtException(thrown Value, site throwSite) {
	if vm.curRealm.exceptionReporter != nil {
		vm.protect(func() {
			vm.curRealm.exceptionReporter(thrown, site.location())
		})
		vm.thrown = Undefined
	}
}
