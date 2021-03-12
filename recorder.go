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
// "command" and an "output"; see grammar below). These recordings can
// then be played back, which provides a handy way to mock out the components
// being recorded.
//
// Users will typically want to embed a Recorder into structs that oversee the
// sort of side-effect or I/O they'd like to record and later playback (instead
// of "doing the real thing" in tests). These side-effects could be pretty much
// anything. If we're building a CLI that calls into the filesystem to filter
// for a set of files and writes out their contents to a zip file, the "I/O"
// would be the listing out of files, and the side-effects would include
// writing the zip file.
//
// I/O could also be outside-of-package boundaries that a stand-alone component
// calls out to. Recorders, if embedded into the component in question, lets us:
//  (a) Record the set of outbound calls, and the relevant responses, while
//  "doing the real thing".
//  (b) Play back from earlier recordings, intercepting all outbound calls and
//  effecting mock out all dependencies the component has.
//
// Let us try and mock out a globber. Broadly what it could look like is as
// follows:
//
//      type globber struct {
//          *recorder.Recorder
//      }
//
//      // glob returns the names of all files matching the given pattern.
//      func (g *globber) glob(pattern string) ([]string, error) {
//          output, err := g.Next(pattern, func() (string, error) {
//              matches, err := filepath.Glob(pattern) // do the real thing
//              if err != nil {
//                  return "", err
//              }
//
//              output := fmt.Sprintf("%s\n", strings.Join(matches, "\n"))
//              return output, nil
//          })
//          if err != nil {
//              return nil, err
//          }
//
//          matches := strings.Split(strings.TrimSpace(output), "\n")
//          return matches, nil
//      }
//
// We had to define tiny bi-directional parsers to convert our input and output
// to the human-readable string form Recorders understand. Strung together we
// can build tests that would plumb in Recorders with the right mode and play
// back from them if asked for. See example/ for this test pattern, where it
// behaves differently depending on whether or not -record is specified.
//
// 		$ go test -run TestExample [-record]
//		$ cat testdata/recording
// 		testdata/files/*
// 		----
// 		testdata/files/aaa
// 		testdata/files/aab
// 		testdata/files/aac
//
// Once the recordings are captured, they can be edited and maintained by hand.
// An example of where we might want to do this is for recordings for commands
// that generate copious amounts of output. It suffices for us to trim the
// recording down by hand, and make sure we don't re-record over it (by
// inspecting the diffs during review). Recordings, like other mocks, are also
// expected to get checked in as test data fixtures.
//
// ---
//
// The printed form of the command is defined by the following grammar. This
// form is used when generating/reading from recording files.
//
//   # comment
//   <command> \
//   <that wraps over onto the next line>
//   ----
//   <output>
//
// By default <output> cannot contain blank lines. This alternative syntax
// allows the use of blank lines.
//
//   <command>
//   ----
//   ----
//   <output>
//
//   <more output>
//   ----
//   ----
//
// Callers are free to use <output> to model errors as well; it's all opaque to
// Recorders.
type Recorder struct {
	// writer is set if we're in recording mode, and is where operations are
	// recorded.
	writer io.Writer

	// scanner and op are set if we're in replay mode. It's where we're
	// replaying the recording from. op is the scratch space used to
	// parse out the current operation being read.
	scanner *scanner
	op      operation
}

// New constructs a Recorder, using the specified configuration option (either
// WithReplay or WithRecording).
func New(opt Option) *Recorder {
	r := &Recorder{}
	opt(r)
	return r
}

// Option is used to configure a new Recorder.
type Option func(r *Recorder)

// WithReplay is used to configure a Recorder to play back from the given
// io.Reader. The provided name is used only for diagnostic purposes, it's
// typically the name of the recording file being read.
func WithReplay(from io.Reader, name string) Option {
	return func(re *Recorder) {
		re.scanner = newScanner(from, name)
	}
}

// WithRecording is used to configure a Recorder to record into the given
// io.Writer. The recordings can then later be replayed from (see WithReplay).
func WithRecording(to io.Writer) Option {
	return func(r *Recorder) {
		r.writer = to
	}
}

// Next is used to step through the next operation in the recorder. It does one
// of three things, depending on how the recorder is configured.
//  (a) WithReplay replays the next command in the recording, as long as it's
//  identical to the provided one
//  (b) WithRecording records the given command and output (captured by the
//  provided callback)
//  (c) If the recorder is nil (i.e. it's simply not configured), it will
//  transparently execute the callback
func (r *Recorder) Next(command string, f func() (output string, err error)) (string, error) {
	if r == nil {
		// Do the real thing; we're not recording or replaying.
		output, err := f()
		return output, err
	}

	if r.recording() {
		output, err := f()
		if err != nil {
			return "", err
		}

		op := operation{command, output}
		if err := r.record(op); err != nil {
			return "", err
		}
		return output, nil
	}

	var output string
	found, err := r.step(func(op operation) error {
		if op.command != command {
			return fmt.Errorf("%s: expected %q, got %q", r.scanner.pos(), op.command, command)
		}
		output = op.output
		return nil
	})
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("%s: recording for %q not found", r.scanner.pos(), command)
	}

	return output, nil
}

// recording returns whether or not the recorder is configured to record (as
// opposed to being configured to replay from an existing recording).
func (r *Recorder) recording() bool {
	return r.writer != nil
}

// record is used to record the given operation.
func (r *Recorder) record(op operation) error {
	if !r.recording() {
		return errors.New("misconfigured recorder; not set to record")
	}

	_, err := r.writer.Write([]byte(op.String()))
	return err
}

// step is used to iterate through the next operation found in the recording, if
// any.
func (r *Recorder) step(f func(operation) error) (found bool, err error) {
	if r.recording() {
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

// parseOperation parses out the next operation from the internal scanner. See
// top-level comment on Recorder to understand the grammar we're parsing
// against.
func (r *Recorder) parseOperation() (parsed bool, err error) {
	for r.scanner.Scan() {
		r.op = operation{}
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
		r.op.command = command

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
// top-level comment on Recorder to understand the grammar we're parsing
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
// correctly. See top-level comment on Recorder to understand the grammar we're
// parsing against.
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

// parseOutput parses an <output>. See top-level comment on Recorder to
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
		r.op.output = buf.String()
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
			r.op.output = buf.String()
			return nil
		}

		// The separator we saw was part of the command output.
		// Let's collect both lines (the first separator, and the
		// new one), and continue.
		if _, err := fmt.Fprintln(&buf, line); err != nil {
			return err
		}

		line2 := r.scanner.Text()
		if _, err := fmt.Fprintln(&buf, line2); err != nil {
			return err
		}
	}

	// We reached the end of the file before finding the closing separator.
	return errors.New(fmt.Sprintf("%s: missing closing double ---- separators", r.scanner.pos()))
}

// TODO(irfansharif): We could introduce a `# keep` directive to pin recordings
// on re-write. It raises a few questions around how new recordings get merged
// with existing ones, but it could be useful. It would allow test authors to
// trim down auto-generated mocks by hand for readability, and ensure that
// re-writes don't simply undo the work.
