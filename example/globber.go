package example

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/irfansharif/recorder"
)

type globber struct {
	*recorder.Recorder
}

func (g *globber) glob(pattern string) (matches []string) {
	if g.Recorder == nil || g.Recording() {
		// Do the real thing.
		matches, _ = filepath.Glob(pattern)
	}

	if g.Recorder == nil {
		return matches
	}

	if g.Recording() {
		g.record(pattern, matches)
		return matches
	}

	return g.replay(pattern)
}

func (g *globber) record(pattern string, matches []string) {
	op := recorder.Operation{
		Command: pattern,
		Output:  fmt.Sprintf("%s\n", strings.Join(matches, "\n")),
	}
	g.Record(op)
}

func (g *globber) replay(pattern string) (matches []string) {
	found, _ := g.Next(func(op recorder.Operation) error {
		if op.Command != pattern {
		} // expected op.Command, got pattern
		output := strings.TrimSpace(op.Output)
		matches = strings.Split(output, "\n")
		return nil
	})
	if !found {
	} // recording for pattern not found
	return matches
}
