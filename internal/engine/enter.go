// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"crypto/subtle"

	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/xerrors"
)

// hostContext is the host state in effect during an entry into the agent.
type hostContext struct {
	continuationData any

	// enteredRealm stays fixed across ordinary cross-realm calls.
	enteredRealm *realm
}

// enterJob installs the job's host state across calls in its execution realm.
func (cx *ExecutionContext) enterJob(continuationData any, f func()) {
	// The outer host call already entered the only realm in a single-realm agent.
	if cx.trackJobRealms {
		caller := cx.activate()
		entered := cx.enteredRealm
		cx.enteredRealm = cx.realm
		defer func() {
			cx.enteredRealm = entered
			caller.activate()
		}()
	}
	previous := cx.continuationData
	cx.continuationData = continuationData
	defer cx.SetContinuationData(previous)
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
func (cx *ExecutionContext) enter(f func() storage.Value) (result storage.Value, err error) {
	if previous := cx.activate(); previous != cx {
		defer previous.activate()
	}
	if cx.entered > 0 {
		return cx.enterBoundary(f)
	}
	cx.enteredRealm = cx.realm
	cx.captureThrowSites = cx.realm.exceptionReporter != nil
	defer func() {
		cx.enteredRealm = nil
		cx.captureThrowSites = false
		cx.thrownAt = throwSite{}
	}()
	// Data independent timing stays enabled for the goroutine across rescheduling, so the
	// nested calls need nothing more.
	subtle.WithDataIndependentTiming(func() {
		result, err = cx.enterBoundary(f)
	})
	return result, err
}

// enterBoundary implements [ExecutionContext.enter].
func (cx *ExecutionContext) enterBoundary(f func() storage.Value) (result storage.Value, err error) {
	// A host callback that recovered an interruption runs no more JavaScript.
	cx.ResumeInterruption()
	frames, natives, sp, depth := len(cx.frames), len(cx.natives), cx.sp, cx.depth
	callerRealm := cx.callerRealm
	thrown, site := cx.thrown, cx.thrownAt
	hostContext := cx.hostContext
	outermost := cx.entered == 0
	cx.entered++
	defer func() {
		cx.entered--
		if outermost {
			defer func() {
				if cx.abortCause != nil {
					cause := cx.abortCause
					cx.abortCause = nil
					if internal, ok := cause.(*xerrors.InternalError); ok {
						result, err = storage.Undefined, internal
					}
					cx.abortModuleEvaluations(cause)
					cx.dropJobs()
				}
				cx.Raised = nil
				cx.ClearKeptObjects()
				cx.clearStale()
				cx.releaseStack()
			}()
		}
		if r := recover(); r != nil {
			sig, interrupted := r.(*storage.InterruptSignal)
			internal, failed := r.(*xerrors.InternalError)
			cx.hostContext = hostContext
			// Only activations belonging to this boundary have lost their Go callers.
			cx.activate()
			for i := frames; i < len(cx.frames); i++ {
				if g := cx.frames[i].gen; g != nil {
					g.abort()
				}
			}
			cx.unwindTo(frames)
			cx.truncateNatives(natives)
			cx.sp, cx.depth = sp, depth
			cx.callerRealm = callerRealm
			cx.thrown, cx.thrownAt = thrown, site
			var cause error
			if interrupted {
				if !outermost {
					panic(r)
				}
				result, err = storage.Undefined, &InterruptedError{
					Cause: sig.Cause,
				}
				cause = sig.Cause
				if cause == nil {
					cause = err
				}
			} else if failed {
				cx.abortCause = internal
				if !outermost {
					panic(r)
				}
				return
			} else {
				cx.abortCause = &PanicError{
					Value: r,
				}
				panic(r)
			}
			// The rejections of the modules queue jobs, which are dropped as well.
			cx.abortModuleEvaluations(cause)
			cx.dropJobs()
		}
	}()
	if !cx.protect(func() {
		ok := cx.protect(func() {
			result = f()
		})
		cx.ResumeInterruption()
		thrown := cx.takeThrown()
		site := cx.thrownAt
		if !ok && outermost {
			cx.reportUncaughtException(thrown, site)
		}
		cx.runJobs()
		cx.ResumeInterruption()
		if !ok {
			cx.thrown, cx.thrownAt = thrown, site
			panic(errThrow)
		}
	}) {
		return storage.Undefined, cx.takeException()
	}
	return result, nil
}

func (cx *ExecutionContext) reportUncaughtException(thrown storage.Value, site throwSite) {
	if cx.realm.exceptionReporter != nil {
		cx.protect(func() {
			cx.realm.exceptionReporter(thrown, site.location())
		})
		cx.thrown = storage.Undefined
	}
}
