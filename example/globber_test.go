package example

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/irfansharif/recorder"
	"github.com/stretchr/testify/require"
)

var recordFlag = flag.Bool(
	"record", false,
	"ignore existing recordings and rewrite them with results from an actual execution",
)

func TestExample(t *testing.T) {
	path := "testdata/recording"
	pattern := "testdata/files/*"
	matches, err := filepath.Glob(pattern)
	require.Nil(t, err)

	var rec *recorder.Recorder
	if *recordFlag {
		recording, err := os.Create(path)
		require.Nil(t, err)
		defer func() {
			require.Nil(t, recording.Close())
		}()

		rec = recorder.New(recorder.WithRecording(recording))
	} else {
		recording, err := os.Open(path)
		require.Nil(t, err)
		defer func() {
			require.Nil(t, recording.Close())
		}()

		rec = recorder.New(recorder.WithReplay(recording, path))
	}

	g := globber{rec}
	results, err := g.glob(pattern)
	require.Nil(t, err)
	require.Equal(t, matches, results)
}
