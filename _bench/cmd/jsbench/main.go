// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Jsbench runs the benchmarks of this module on every engine and reports their times relative
// to Ekuma. It runs in the directory of the module:
//
//	go run ./cmd/jsbench [flags]
//
// First, it fetches the third-party benchmark programs and builds QuickJS in C into the
// directory that the benchmarks read them from: $EKUMA_BENCH, or ekuma/bench in the user cache
// directory. Building QuickJS needs make and a C compiler. With -setup, it does only this, after
// which go test -bench runs the benchmarks directly.
//
// A round runs each benchmark once on every engine, each in a process of its own, in an order
// that rotates with the round and the benchmark. Each suite writes its output in go test format
// to the results directory, using the lowercase suite name with a .txt extension.
// The report reads these files.
// For each benchmark and engine, the report gives the median over the rounds of the time
// relative to Ekuma's in the same round, and for each suite and engine, the geometric mean of
// these over the benchmarks that the engine ran.
//
// The report is printed as Markdown sections between marker comments. With -update, the
// sections replace the ones between the same markers in the given files. With -report, the
// report is made without running the benchmarks.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"

	"github.com/hajimehoshi/ekuma/_bench/internal/comparison"
)

// A suite is a benchmark function of the module.
type suite struct {
	// name is the name of the benchmark function without "Benchmark".
	name string

	// title is the name of the suite in the report.
	title string

	// benchtime is the -test.benchtime of each run of a benchmark.
	benchtime string

	// rounds is the number of rounds by default.
	rounds int
}

func (s suite) resultsPath(dir string) string {
	return filepath.Join(dir, strings.ToLower(s.name)+".txt")
}

var suites = []suite{
	{
		name:      "V8",
		title:     "V8 benchmark suite v7",
		benchtime: "1s",
		rounds:    5,
	},
	{
		name:      "Micro",
		title:     "QuickJS microbench",
		benchtime: "200ms",
		rounds:    3,
	},
}

// modulePath is the path of the module that this command runs the benchmarks of.
const modulePath = "github.com/hajimehoshi/ekuma/_bench"

var (
	flagSuites  = flag.String("suite", "V8,Micro", "comma-separated `suites` to run")
	flagRounds  = flag.Int("rounds", 0, "number of rounds of each suite, or 0 for the default of the suite")
	flagBench   = flag.String("bench", "", "run only the benchmarks whose names match `regexp`")
	flagEngine  = flag.String("engine", "", "run only the engines whose names match `regexp`")
	flagResults = flag.String("results", filepath.Join("results", runtime.GOOS+"_"+runtime.GOARCH), "`directory` of the results files")
	flagSetup   = flag.Bool("setup", false, "only fetch the benchmark programs and build QuickJS in C")
	flagReport  = flag.Bool("report", false, "report the results files without running the benchmarks")
	flagUpdate  = flag.String("update", "", "comma-separated Markdown `files` whose sections are replaced by the report")
)

func main() {
	log.SetFlags(0)
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := jsbench(ctx); err != nil {
		log.Fatal(err)
	}
}

func jsbench(ctx context.Context) error {
	if *flagSetup && *flagReport {
		return errors.New("-setup and -report cannot be used together")
	}
	if *flagSetup {
		root, err := setUp(ctx)
		if err != nil {
			return err
		}
		fmt.Println(root)
		return nil
	}

	if !*flagReport {
		var selected []suite
		for name := range strings.SplitSeq(*flagSuites, ",") {
			i := slices.IndexFunc(suites, func(s suite) bool {
				return s.name == name
			})
			if i < 0 {
				return fmt.Errorf("unknown suite %q", name)
			}
			selected = append(selected, suites[i])
		}
		var filter *regexp.Regexp
		if *flagBench != "" {
			var err error
			if filter, err = regexp.Compile(*flagBench); err != nil {
				return err
			}
		}
		engines := comparison.Engines
		if *flagEngine != "" {
			re, err := regexp.Compile(*flagEngine)
			if err != nil {
				return err
			}
			engines = slices.DeleteFunc(slices.Clone(engines), func(e string) bool {
				return !re.MatchString(e)
			})
			if len(engines) == 0 {
				return fmt.Errorf("no engines match %q", *flagEngine)
			}
		}
		if err := runSuites(ctx, selected, filter, engines); err != nil {
			return err
		}
	}

	sections, err := report(*flagResults)
	if err != nil {
		return err
	}
	if *flagUpdate == "" {
		for _, name := range sectionNames(sections) {
			fmt.Printf("%s\n\n", marked(name, sections[name]))
		}
		return nil
	}
	for path := range strings.SplitSeq(*flagUpdate, ",") {
		if err := update(path, sections); err != nil {
			return err
		}
	}
	return nil
}

// runSuites runs the benchmarks of suites that match filter, or all of them if filter is nil, on
// engines.
func runSuites(ctx context.Context, suites []suite, filter *regexp.Regexp, engines []string) error {
	b, err := os.ReadFile("go.mod")
	if err != nil || !strings.HasPrefix(string(b), "module "+modulePath+"\n") {
		return fmt.Errorf("run this command in the directory of the module %s", modulePath)
	}

	root, err := setUp(ctx)
	if err != nil {
		return err
	}

	dir, err := os.MkdirTemp("", "jsbench-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	bin, err := buildTest(ctx, dir)
	if err != nil {
		return err
	}
	env, err := environment(ctx, bin)
	if err != nil {
		return err
	}

	var failures []error
	for _, s := range suites {
		names, err := benchmarkNames(s, root)
		if err != nil {
			return err
		}
		if filter != nil {
			names = slices.DeleteFunc(names, func(name string) bool {
				return !filter.MatchString(name)
			})
		}
		if len(names) == 0 {
			return fmt.Errorf("%s: no benchmarks match %q", s.name, *flagBench)
		}
		rounds := s.rounds
		if *flagRounds > 0 {
			rounds = *flagRounds
		}
		if err := runSuite(ctx, bin, s, names, engines, rounds, env, *flagResults); err != nil {
			if errors.Is(err, errBaselineFailed) {
				failures = append(failures, err)
				continue
			}
			return err
		}
	}
	return errors.Join(failures...)
}

// setUp fetches the third-party benchmark programs and builds QuickJS in C, unless they are
// there already, and returns the directory that holds them.
func setUp(ctx context.Context) (string, error) {
	root, err := comparison.Root()
	if err != nil {
		return "", err
	}
	for _, s := range []*comparison.Suite{comparison.V8, comparison.Micro} {
		if err := s.Fetch(ctx, root); err != nil {
			return "", err
		}
	}
	if err := comparison.BuildQuickJS(ctx, root); err != nil {
		return "", err
	}
	return root, nil
}

// benchmarkNames returns the names of the benchmarks of s, whose programs are in root.
func benchmarkNames(s suite, root string) ([]string, error) {
	switch s.name {
	case "V8":
		var names []string
		for _, b := range comparison.V8Benchmarks {
			names = append(names, b.Name)
		}
		return names, nil
	case "Micro":
		srcs, err := comparison.Micro.Load(root)
		if err != nil {
			return nil, err
		}
		_, names, err := comparison.Microbench(srcs["microbench.js"])
		return names, err
	}
	return nil, errors.New("unknown suite " + s.name)
}
