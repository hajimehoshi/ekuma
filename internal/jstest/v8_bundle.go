// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package jstest

import (
	"context"
	"fmt"
	"iter"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// A test shares bundle sources across its files and realms. Each realm's
// module loader keeps its own parsed module records.
type v8Bundle struct {
	modules       map[string]string
	nextAnonymous int
}

const v8BundleMarker = "// JS_BUNDLE_"

// splitV8Bundle returns the marker and source of each part, or nil for an ordinary script.
func splitV8Bundle(src string) iter.Seq2[string, string] {
	for {
		src = strings.TrimLeft(src, " \t\n\v\f\r")
		if strings.HasPrefix(src, v8BundleMarker) {
			break
		}
		var end string
		switch {
		case strings.HasPrefix(src, "//"):
			end = "\n"
		case strings.HasPrefix(src, "/*"):
			end = "*/"
		default:
			return nil
		}
		var ok bool
		_, src, ok = strings.Cut(src[2:], end)
		if !ok {
			return nil
		}
	}
	return func(yield func(string, string) bool) {
		for rest := src; rest != ""; {
			header, body, newline := strings.Cut(rest[len(v8BundleMarker):], "\n")
			if newline {
				header = strings.TrimSuffix(header, "\r")
			}
			// After the first marker d8 looks for the literal prefix anywhere.
			i := strings.Index(body, v8BundleMarker)
			if i < 0 {
				i = len(body)
			}
			if !yield(header, body[:i]) {
				return
			}
			rest = body[i:]
		}
	}
}

type v8BundleEntry struct {
	script string
	module string
}

func (b *v8Bundle) register(cwd string, parts iter.Seq2[string, string]) ([]v8BundleEntry, error) {
	var order []v8BundleEntry
	for header, src := range parts {
		// d8 passes each part to V8 as a NUL-terminated string.
		src, _, _ = strings.Cut(src, "\x00")
		var name string
		entrypoint := strings.HasPrefix(header, "MODULE_ENTRYPOINT")
		switch {
		case header == "SCRIPT":
			order = append(order, v8BundleEntry{
				script: src,
			})
			continue
		case entrypoint:
			var named bool
			name, named = strings.CutPrefix(header, "MODULE_ENTRYPOINT:")
			if !named {
				name = fmt.Sprintf("entrypoint_%d.mjs", b.nextAnonymous)
				b.nextAnonymous++
			}
		case strings.HasPrefix(header, "MODULE:"):
			name = strings.TrimPrefix(header, "MODULE:")
		default:
			continue
		}
		name = v8ModuleName(cwd, name)
		if _, exists := b.modules[name]; exists {
			return nil, fmt.Errorf("duplicate bundle module: %s", name)
		}
		b.modules[name] = src
		if entrypoint {
			order = append(order, v8BundleEntry{
				module: name,
			})
		}
	}
	return order, nil
}

func (s *v8Scripts) runBundle(ctx context.Context, loop *v8EventLoop, file string, parts iter.Seq2[string, string]) suiteResult {
	order, err := s.bundle.register(s.cwd, parts)
	if err != nil {
		return suiteFailed.result(err.Error())
	}
	var result suiteResult
	for _, item := range order {
		var r suiteResult
		if item.module != "" {
			r = s.runModule(ctx, loop, item.module)
		} else {
			r = s.runScript(ctx, loop, file, item.script)
		}
		if r.outcome == suiteBroken {
			return r
		}
		if result.outcome == suitePassed {
			result = r
		}
		if loop.terminated {
			break
		}
	}
	return result
}

// CheckBundle checks module identity and execution order across bundle and file boundaries.
func (host *V8) CheckBundle(t *testing.T) {
	test := v8Test{
		checkoutDir: filepath.Join(t.TempDir(), "v8 checkout"),
		file:        "test/mjsunit/bundle.js",
		flags:       []string{"--bundle"},
	}
	checkout := fstest.MapFS{
		"test/mjsunit/helper.js": {
			Data: []byte(fmt.Sprintf(`/* preamble */
// JS_BUNDLE_SCRIPT
const cwd = %q;
let order = 'S';
let fromScript;
import('./sub/entry.mjs').then(ns => { fromScript = ns; });
// JS_BUNDLE_MODULE_ENTRYPOINT:test/mjsunit/sub/entry.mjs
export { value } from '\\\\deps\\bundle.mjs';
export const url = import.meta.url;
order += 'E';
// JS_BUNDLE_SCRIPT
order += 'T';
// JS_BUNDLE_MODULE_ENTRYPOINT
import * as entry from 'test/mjsunit/sub/entry.mjs';
if (import.meta.url !== cwd + '/entrypoint_0.mjs') throw Error(import.meta.url);
if (entry !== fromScript || entry.value !== 42) throw Error('module identity');
order += 'A';
// JS_BUNDLE_MODULE:test/mjsunit/sub/deps/bundle.mjs
export { value } from 'https://bundle.test/dep';
// JS_BUNDLE_MODULE:https://bundle.test/dep
if (import.meta.url !== 'https://bundle.test/dep') throw Error(import.meta.url);
export { value } from 'data:text/javascript,export const value = 0';
// JS_BUNDLE_MODULE:data:text/javascript,export const value = 0
export const value = 42;`, filepath.ToSlash(test.checkoutDir))),
		},
		"plain.mjs": {
			Data: []byte(`// JS_BUNDLE_SCRIPT
if (import.meta.url !== cwd + '/plain.mjs') throw Error(import.meta.url);
if (await import(fromScript.url) !== fromScript) throw Error('absolute import');
export const value = (order += 'M');`),
		},
		test.file: {
			Data: []byte(`// Flags: --bundle
// Files: test/mjsunit/helper.js plain.mjs
order += 'F';
// JS_BUNDLE_SCRIPT
if (order !== 'SETAMF') throw Error(order);`),
		},
	}
	if result := test.run(t.Context(), host, checkout, testTimeout); result.outcome != suitePassed {
		t.Error(result.reason)
	}
	for _, tc := range []struct {
		name      string
		body      string
		wantPrint string
	}{
		{
			name: "duplicate before execution",
			body: `// JS_BUNDLE_SCRIPT
print('ran before registration finished');
// JS_BUNDLE_MODULE:sub/../dup.mjs
// JS_BUNDLE_MODULE_ENTRYPOINT:dup.mjs`,
		},
		{
			name: "continue after an exception",
			body: `// JS_BUNDLE_SCRIPT
throw 1;
// JS_BUNDLE_MODULE_ENTRYPOINT
print('continued');`,
			wantPrint: "[continued]",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkout := fstest.MapFS{
				test.file: {
					Data: []byte("// Flags: --bundle\n" + tc.body),
				},
			}
			result, printed := test.execute(t.Context(), host, checkout, testTimeout)
			if result.outcome != suiteFailed || printed != tc.wantPrint {
				t.Errorf("got %v, print %q; want a failure, print %q", result, printed, tc.wantPrint)
			}
		})
	}
}
