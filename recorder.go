// Copyright 2021 Irfan Sharif.
// Copyright 2018 The Cockroach Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
// implied. See the License for the specific language governing
// permissions and limitations under the License.
//
// Portions of this code was derived from cockroachdb/datadriven.

package recorder

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Recorder can be used to record a set of operations (defined only by a
// "command" and an "expected output"). These recordings can then be played
// back, which provides a handy way to mock out the components being recorded.
//
// Users will typically want to embed a Recorder into a component that oversees
// any sort of side-effects or I/O they'd like to record, and later playback
// (instead of "doing the real thing" in tests).
//
// These side-effects could be pretty much anything. Say for example we're
// building a CLI that calls into the filesystem to filter for a set of
// directories and writes out a zip file. The "I/O" here would be the listing
// out of directories, and the side-effects would include creating the zip file.
//
// This "I/O" could also be outside-of-package boundaries a stand-alone
// component calls into. Recorder, if embedded into the component in question,
// lets us:
//
// (a) Record the set of outbound calls, and the relevant responses, while
//     "doing the real thing".
// (b) Play back from an earlier recording, intercepting all outbound calls and
//     effecting mock out all dependencies the component has.
//
// Let us try and mock out a globber. Broadly what it could look like is as
// follows:
//
//      type globber struct {
//        *recorder.Recorder
//      }
//
//      func (g *globber) glob(pattern string) (matches []string) {
//          if g.Recorder == nil || g.Recording() {
//              // Do the real thing.
//              matches, _ = filepath.Glob(pattern)
//          }
//
//          if g.Recording() {
//              g.record(pattern, matches)
//              return matches
//          }
//
//          return g.replay(pattern)
//      }
//
// We'll have to define tiny "parsers" to go convert our input/output to the
// simple string representation Recorders understand.
//
//      func (g *globber) record(pattern string, matches []string) {
//          op := recorder.Operation{
//              Command: pattern,
//              Output:  fmt.Sprintf("%s\n", strings.Join(matches, "\n")),
//          }
//          g.Record(op)
//      }
//
//      func (g *globber) replay(pattern string) (matches []string) {
//          found, _ := g.Next(func(op recorder.Operation) error {
//              if op.Command != pattern {
//              } // expected op.Command, got pattern
//              output := strings.TrimSpace(op.Output)
//              matches = strings.Split(output, "\n")
//              return nil
//          })
//          if !found {
//          } // recording for pattern not found
//          return matches
//      }
//
// Strung together we could construct tests that would plumb in Recorders with
// the right mode (say if the test is run using -record, it would emit files),
// and then playback from recordings if asked for (reading from previously
// emitted files). See example/ for this exact testing pattern.
//
// 		testdata/files/*
// 		----
// 		----
// 		testdata/files/aaa
// 		testdata/files/aab
// 		testdata/files/aac
// 		----
// 		----
type Recorder struct {
	// writer is set if we're in recording mode, and is where operations are
	// recorded.
	writer io.Writer

	// scanner and op are set if we're in replay mode. It's where we're
	// replaying the recording from. op is the scratch space used to
	// parse out the current operation being read.
	scanner *scanner
	op      Operation
}

// New constructs a Recorder, using the specified configuration option (either
// WithReplayFrom or WithRecordingTo).
func New(opt func(r *Recorder)) *Recorder {
	r := &Recorder{}
	opt(r)
	return r
}

// WithReplayFrom is used to configure a Recorder to play back from the given
// io.Reader. The provided name is used only for diagnostic purposes, it's
// typically the name of the recording file being read.
func WithReplayFrom(r io.Reader, name string) func(*Recorder) {
	return func(re *Recorder) {
		re.scanner = newScanner(r, name)
	}
}

// WithRecordingTo is used to configure a Recorder to record into the given
// io.Writer. The recordings can then later be replayed from (see
// WithReplayFrom).
func WithRecordingTo(w io.Writer) func(*Recorder) {
	return func(r *Recorder) {
		r.writer = w
	}
}

// Recording returns whether or not the recorder is configured to record (as
// opposed to being configured to replay from an existing recording).
func (r *Recorder) Recording() bool {
	return r.writer != nil
}

// Record is used to record the given operation.
func (r *Recorder) Record(o Operation) error {
	if !r.Recording() {
		return errors.New("misconfigured recorder; not set to record")
	}

	_, err := r.writer.Write([]byte(o.String()))
	return err
}

// Next is used to step through the next Operation found in the recording, if
// any.
func (r *Recorder) Next(f func(Operation) error) (found bool, err error) {
	if r.Recording() {
		return false, errors.New("misconfigured recorder; set to record, not replay")
	}

	parsed, err := r.parseOperation()
	if err != nil {
		return false, err
	}

	if !parsed {
		return false, nil
	}

	if err := f(r.op); err != nil {
		return false, fmt.Errorf("%s: %w", r.scanner.pos(), err)
	}
	return true, nil
}

// parseOperation parses out the next Operation from the internal scanner. See
// type-level comment on Operation to understand the grammar we're parsing
// against.
func (r *Recorder) parseOperation() (parsed bool, err error) {
	for r.scanner.Scan() {
		r.op = Operation{}
		line := r.scanner.Text()

		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			// Skip comment lines.
			continue
		}

		// Support wrapping command directive lines using "\".
		for strings.HasSuffix(line, `\`) && r.scanner.Scan() {
			nextLine := r.scanner.Text()
			line = strings.TrimSuffix(line, `\`)
			line = strings.TrimSpace(line)
			line = fmt.Sprintf("%s %s", line, strings.TrimSpace(nextLine))
		}

		command, err := r.parseCommand(line)
		if err != nil {
			return false, err
		}
		if command == "" {
			// Nothing to do here.
			continue
		}
		r.op.Command = command

		if err := r.parseSeparator(); err != nil {
			return false, err
		}

		if err := r.parseOutput(); err != nil {
			return false, err
		}

		return true, nil
	}
	return false, nil
}

// parseCommand parses a <command> line and returns it if parsed correctly. See
// type-level comment on Operation to understand the grammar we're parsing
// against.
func (r *Recorder) parseCommand(line string) (cmd string, err error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", nil
	}

	origLine := line
	cmd = strings.TrimSpace(line)
	if cmd == "" {
		column := len(origLine) - len(line) + 1
		return "", errors.New(fmt.Sprintf("%s: cannot parse command at col %d: %s", r.scanner.pos(), column, origLine))
	}
	return cmd, nil
}

// parseSeparator parses a separator ('----'), erroring out if it's not parsed
// correctly. See type-level comment on Operation to understand the grammar
// we're parsing against.
func (r *Recorder) parseSeparator() error {
	if !r.scanner.Scan() {
		return errors.New(fmt.Sprintf("%s: expected to find separator after command", r.scanner.pos()))
	}
	line := r.scanner.Text()
	if line != "----" {
		return errors.New(fmt.Sprintf("%s: expected to find separator after command, found %q instead", r.scanner.pos(), line))
	}
	return nil
}

// parseOutput parses an <output>. See type-level comment on Operation to
// understand the grammar we're parsing against.
func (r *Recorder) parseOutput() error {
	var buf bytes.Buffer
	var line string

	var allowBlankLines bool
	if r.scanner.Scan() {
		line = r.scanner.Text()
		if line == "----" {
			allowBlankLines = true
		}
	}

	if !allowBlankLines {
		// Terminate on first blank line.
		for {
			if strings.TrimSpace(line) == "" {
				break
			}

			if _, err := fmt.Fprintln(&buf, line); err != nil {
				return err
			}

			if !r.scanner.Scan() {
				break
			}

			line = r.scanner.Text()
		}
		r.op.Output = buf.String()
		return nil
	}

	// Look for two successive lines of "----" before terminating.
	for r.scanner.Scan() {
		line = r.scanner.Text()
		if line != "----" {
			// We just picked up a regular line that's part of the command
			// output.
			if _, err := fmt.Fprintln(&buf, line); err != nil {
				return err
			}

			continue
		}

		// We picked up a separator. We could either be part of the
		// command output, or it was actually intended by the user as a
		// separator. Let's check to see if we can parse a second one.
		if err := r.parseSeparator(); err == nil {
			// We just saw the second separator, the output portion is done.
			// Read the following blank line.
			if r.scanner.Scan() && r.scanner.Text() != "" {
				return errors.New(fmt.Sprintf("%s: non-blank line after end of double ---- separator section", r.scanner.pos()))
			}
			break
		}

		// The separator we saw was part of the command output.
		// Let's collect both lines (the first separator, and the
		// new one).
		if _, err := fmt.Fprintln(&buf, line); err != nil {
			return err
		}

		line2 := r.scanner.Text()
		if _, err := fmt.Fprintln(&buf, line2); err != nil {
			return err
		}
	}

	r.op.Output = buf.String()
	return nil
}

// TODO(irfansharif): We could introduce a `# keep` directive to pin recordings
// on re-write. It raises a few questions around how new recordings get merged
// with existing ones, but it could be useful. It would allow test authors to
// trim down auto-generated mocks by hand for readability, and ensure that
// re-writes don't simply undo the work.
//
// TODO(irfansharif): If we could model errors, that'd be useful. Same thing for
// top-level test.
