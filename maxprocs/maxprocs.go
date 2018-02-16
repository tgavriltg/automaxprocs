// Copyright (c) 2017 Uber Technologies, Inc.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

// Package maxprocs lets Go programs easily configure runtime.GOMAXPROCS to
// match the configured Linux CPU quota. Unlike the top-level automaxprocs
// package, it lets the caller configure logging and handle errors.
package maxprocs // import "go.uber.org/automaxprocs/maxprocs"

import (
	"os"
	"runtime"

	iruntime "go.uber.org/automaxprocs/internal/runtime"
)

const _maxProcsKey = "GOMAXPROCS"

type config struct {
	printf        func(string, ...interface{})
	procs         func(int) (int, iruntime.CPUQuotaStatus, error)
	minGOMAXPROCS int
}

func (c *config) log(fmt string, args ...interface{}) {
	if c.printf != nil {
		c.printf(fmt, args...)
	}
}

// An Option alters the behavior of Set.
type Option interface {
	apply(*config)
}

// Logger uses the supplied printf implementation for log output. By default,
// Set doesn't log anything.
func Logger(printf func(string, ...interface{})) Option {
	return optionFunc(func(cfg *config) {
		cfg.printf = printf
	})
}

// Min sets the minimum GOMAXPROCS value that will be used.
// Any value below 1 is ignored.
func Min(n int) Option {
	return optionFunc(func(cfg *config) {
		if n >= 1 {
			cfg.minGOMAXPROCS = n
		}
	})
}

type optionFunc func(*config)

func (of optionFunc) apply(cfg *config) { of(cfg) }

// Set GOMAXPROCS to match the Linux container CPU quota (if any), returning
// any error encountered and an undo function.
//
// Set is a no-op on non-Linux systems and in Linux environments without a
// configured CPU quota.
func Set(opts ...Option) (func(), error) {
	cfg := &config{
		procs:         iruntime.CPUQuotaToGOMAXPROCS,
		minGOMAXPROCS: 1,
	}
	for _, o := range opts {
		o.apply(cfg)
	}

	undoNoop := func() {
		cfg.log("maxprocs: No GOMAXPROCS change to reset")
	}

	// Honor the GOMAXPROCS environment variable if present. Otherwise, amend
	// `runtime.GOMAXPROCS()` with the current process' CPU quota if the OS is
	// Linux, and guarantee a minimum value of 2 to ensure efficiency.
	if max, exists := os.LookupEnv(_maxProcsKey); exists {
		cfg.log("maxprocs: Honoring GOMAXPROCS=%d as set in environment", max)
		return undoNoop, nil
	}

	maxProcs, status, err := cfg.procs(cfg.minGOMAXPROCS)
	if err != nil {
		return undoNoop, err
	}

	prev := runtime.GOMAXPROCS(0)

	if status == iruntime.CPUQuotaUndefined {
		cfg.log("maxprocs: Leaving GOMAXPROCS=%d: CPU quota undefined", prev)
		return undoNoop, nil
	}

	undo := func() {
		cfg.log("maxprocs: Resetting GOMAXPROCS to %d", prev)
		runtime.GOMAXPROCS(prev)
	}

	switch status {
	case iruntime.CPUQuotaMinUsed:
		cfg.log("maxprocs: Updating GOMAXPROCS=%d: using minimum allowed GOMAXPROCS", maxProcs)
	case iruntime.CPUQuotaUsed:
		cfg.log("maxprocs: Updating GOMAXPROCS=%d: determined from CPU quota", maxProcs)
	}

	runtime.GOMAXPROCS(maxProcs)
	return undo, nil
}
