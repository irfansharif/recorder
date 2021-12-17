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

	"github.com/stretchr/testify/require"
)

func TestRecorder(t *testing.T) {
	data := `
command
----
output
`
	reader := New(WithReplay(bytes.NewReader([]byte(data)), "fuzz"))
	var output, command string
	found, err := reader.step(func(op operation) {
		command = op.command
		output = op.output
	})
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "command", command)
	require.Equal(t, "output\n", output)

	buffer := bytes.NewBuffer(nil)
	recorder := New(WithRecording(buffer))
	_, err = recorder.Next(command, func() (string, error) {
		return output, nil
	})
	require.NoError(t, err)

	before, after := strings.TrimSpace(data), strings.TrimSpace(buffer.String())
	require.Equal(t, before, after)
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
		found, err := reader.step(func(op operation) {
			command, output = op.command, op.output
		})
		require.NoError(t, err)
		if !found {
			break // we're at the end of the file
		}
		parsedOps += 1

		// Write out the next operation, just to see that it goes through.
		buffer := bytes.NewBuffer(nil)
		writer := New(WithRecording(buffer))
		require.NoError(t, writer.record(operation{command, output}))

		// Re-read what we just wrote out, just to see we're able to round trip
		// through the recorder.
		reader2 := New(WithReplay(buffer, "fuzz"))
		_, err = reader2.step(func(op operation) {
			require.Equal(t, op.command, command)
			require.Equal(t, op.output, output)
		})
		require.NoError(t, err)
	}

	require.Equal(t, expectedParsedOps, parsedOps)
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
	_, err := reader.step(func(op operation) {})
	require.NotNil(t, err)
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
	_, err := reader.step(func(op operation) {
		command, output = op.command, op.output
	})
	require.NoError(t, err)

	// Write out the next operation, just to see that it goes through.
	buffer := bytes.NewBuffer(nil)
	writer := New(WithRecording(buffer))
	require.NoError(t, writer.record(operation{command, output}))

	// Re-read what we just wrote out, just to see we're able to round trip
	// through the recorder.
	reader2 := New(WithReplay(buffer, "fuzz"))
	_, err = reader2.step(func(op operation) {
		require.Equal(t, op.command, command)
		require.Equal(t, op.output, output)
	})
	require.NoError(t, err)
}

func TestOperationString(t *testing.T) {
	op := operation{
		command: "test-cmd",
		output:  "",
	}
	expected := `
test-cmd
----

`
	require.Equal(t, strings.TrimLeft(expected, "\n"), op.String())
}
