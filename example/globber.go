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

// glob returns the names of all files matching the given pattern.
func (g *globber) glob(pattern string) ([]string, error) {
	output, err := g.Next(pattern, func() (string, error) {
		matches, err := filepath.Glob(pattern) // do the real thing
		if err != nil {
			return "", err
		}

		output := fmt.Sprintf("%s\n", strings.Join(matches, "\n"))
		return output, nil
	})
	if err != nil {
		return nil, err
	}

	matches := strings.Split(strings.TrimSpace(output), "\n")
	return matches, nil
}
