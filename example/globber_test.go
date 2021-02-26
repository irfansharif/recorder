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
	"ignore existing recordings and rewrite them with results from an actual execution (see -from-checkout)",
)

func TestExample(t *testing.T) {
	path := "testdata/recording"
	pattern := "testdata/files/*"
	matches, err := filepath.Glob(pattern)
	require.Nil(t, err)

	if *recordFlag {
		recording, err := os.Create(path)
		require.Nil(t, err)

		rec := recorder.New(recorder.WithRecordingTo(recording))
		g := globber{rec}
		require.Equal(t, matches, g.glob(pattern))
		require.Nil(t, recording.Close())
		return
	}

	recording, err := os.Open(path)
	require.Nil(t, err)

	rec := recorder.New(recorder.WithReplayFrom(recording, path))
	g := globber{rec}
	require.Equal(t, matches, g.glob(pattern))
	require.Nil(t, recording.Close())
}
