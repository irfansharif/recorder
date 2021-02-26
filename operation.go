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
	"strings"
)

// Operation represents the base unit of what can be recorded. It consists of a
// command and the corresponding output.
//
// The printed form of the command is defined by the following grammar. This
// grammar is used when generating/reading from recording files.
//
//   # comment
//   <command> \
//   <command wraps over onto the next line>
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
type Operation struct {
	Command string // <command>
	Output  string // <output>
}

// String returns a printable form for the given Operation, respecting the
// pre-defined grammar (see type-level comment for the grammar we're
// constructing against).
func (o *Operation) String() string {
	var sb strings.Builder
	sb.WriteString(o.Command)
	sb.WriteString("\n")

	sb.WriteString("----")
	sb.WriteString("\n")

	var emptyLine bool
	lines := strings.Split(strings.TrimRight(o.Output, "\n"), "\n")
	for _, line := range lines {
		if line == "" {
			emptyLine = true
			break
		}
	}
	if emptyLine {
		sb.WriteString("----")
		sb.WriteString("\n")
	}

	sb.WriteString(o.Output)
	if o.Output != "" && !strings.HasSuffix(o.Output, "\n") {
		// If the output is not \n terminated, add it.
		sb.WriteString("\n")
	}

	if emptyLine {
		sb.WriteString("----")
		sb.WriteString("\n")
		sb.WriteString("----")
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	return sb.String()
}
