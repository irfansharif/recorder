// Copyright 2021 Irfan Sharif.
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

package recorder

import (
	"bytes"
	"strings"
	"testing"
)

func TestRecorder(t *testing.T) {
	data := `
command
----
output
`
	reader := New(WithReplay(bytes.NewReader([]byte(data)), "fuzz"))
	var output, command string
	found, err := reader.step(func(op operation) error {
		command = op.command
		output = op.output
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if !found {
		t.Fatal("expected to find operation")
	}
	if command != "command" {
		t.Fatalf("expected %q, got %q", "command", command)
	}
	if output != "output\n" {
		t.Fatalf("expected %q, got %q", "output", output)
	}

	buffer := bytes.NewBuffer(nil)
	writer := New(WithRecording(buffer))
	_, err = writer.Next(command, func() (string, error) {
		return output, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	before, after := strings.TrimSpace(data), strings.TrimSpace(buffer.String())
	if before != after {
		t.Fatalf("mismatched buffers %q and %q", before, after)
	}
}

func TestRecorderMultiple(t *testing.T) {
	data := `
command
----
output

command
----
----
output

output
----
----
`

	const expectedParsedOps = 2
	reader := New(WithReplay(bytes.NewReader([]byte(data)), "fuzz"))
	parsedOps := 0
	for {
		// Parse out the next operation.
		var output, command string
		found, err := reader.step(func(op operation) error {
			command, output = op.command, op.output
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if !found {
			// We're at the end of the file.
			break
		}
		parsedOps += 1

		// Write out the next operation, just to see that it goes through.
		buffer := bytes.NewBuffer(nil)
		writer := New(WithRecording(buffer))
		if err := writer.record(operation{command, output}); err != nil {
			t.Fatal(err)
		}

		// Re-read what we just wrote out, just to see we're able to round trip
		// through the recorder.
		reader2 := New(WithReplay(buffer, "fuzz"))
		_, err = reader2.step(func(op operation) error {
			if op.command != command {
				t.Fatalf("mismatched command: expected %q, got %q", command, op.command)
			}
			if op.output != output {
				t.Fatalf("mismatched output: expected %q, got %q", output, op.output)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	if parsedOps != expectedParsedOps {
		t.Fatalf("expected to found %d operations, found %d", expectedParsedOps, parsedOps)
	}
}

func TestRecorderMalformed(t *testing.T) {
	data := `
0
----
----
1


1
`

	reader := New(WithReplay(bytes.NewReader([]byte(data)), "fuzz"))
	_, err := reader.step(func(op operation) error {
		return nil
	})
	if err == nil {
		t.Fatal("expected non-nil error over malformed data")
	}
}

func TestRecorderParse(t *testing.T) {
	t.Skip("we don't handle trailing newlines well, we strip them off")

	data := `
0
----
----
f




----
----
`

	var output, command string
	reader := New(WithReplay(bytes.NewReader([]byte(data)), "fuzz"))
	_, err := reader.step(func(op operation) error {
		command, output = op.command, op.output
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Write out the next operation, just to see that it goes through.
	buffer := bytes.NewBuffer(nil)
	writer := New(WithRecording(buffer))
	if err := writer.record(operation{command, output}); err != nil {
		t.Fatal(err)
	}

	// Re-read what we just wrote out, just to see we're able to round trip
	// through the recorder.
	reader2 := New(WithReplay(buffer, "fuzz"))
	_, err = reader2.step(func(op operation) error {
		if op.command != command {
			t.Fatalf("mismatched command: expected %q, got %q", command, op.command)
		}
		if op.output != output {
			t.Fatalf("mismatched output: expected %q, got %q", output, op.output)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
