package machine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func cutOf(cuts []domain.OutputCut, stream, reason string) *domain.OutputCut {
	for i := range cuts {
		if cuts[i].Stream == stream && cuts[i].Reason == reason {
			return &cuts[i]
		}
	}
	return nil
}

// Nothing is silently dropped (KUA-137): a run records what Kuayle left out
// of its stored output, how much and why.
func TestOutputCutsRecordWhatWasLeftOut(t *testing.T) {
	output, _ := collectAgentOutput(nil)
	require.Empty(t, output.Cuts(), "a run whose output is stored whole records no cut")

	// A plain line over 16 KiB is omitted whole.
	plain := strings.Repeat("x", 40<<10) + "\n"
	require.NoError(t, output.Write("stdout", []byte("first\n"+plain+"second\n")))
	// A JSON event over 16 KiB keeps its event and loses the middle of its output.
	event := openCodeToolLine(t, "go test ./...", strings.Repeat("0123456789abcdef\n", 100<<10/17))
	require.NoError(t, output.Write("stdout", []byte(event)))
	require.NoError(t, output.Write("stderr", []byte(plain+plain)))
	require.NoError(t, output.Finish())

	cuts := output.Cuts()
	omitted := cutOf(cuts, "stdout", domain.OutputCutLine)
	require.NotNil(t, omitted)
	require.Equal(t, 1, omitted.Count)
	require.EqualValues(t, len(plain), omitted.Bytes)
	require.EqualValues(t, 2, omitted.FirstLine, "the marker stored in its place is the second record")
	require.EqualValues(t, maxAgentOutputLine, omitted.Limit)

	values := cutOf(cuts, "stdout", domain.OutputCutValues)
	require.NotNil(t, values)
	require.Equal(t, 1, values.Count)
	require.Greater(t, values.Bytes, int64(80<<10), "most of the 100 KiB output was cut")
	require.EqualValues(t, 4, values.FirstLine)

	stderr := cutOf(cuts, "stderr", domain.OutputCutLine)
	require.NotNil(t, stderr)
	require.Equal(t, 2, stderr.Count)
	require.EqualValues(t, 2*len(plain), stderr.Bytes)
	require.Nil(t, cutOf(cuts, "stdout", domain.OutputCutRecords))
}

// The 4096-record cap: what came after it is counted, not stored.
func TestOutputCutsRecordTheRecordCap(t *testing.T) {
	output, records := collectAgentOutput(nil)
	var b strings.Builder
	for i := 0; i < maxAgentOutputRecords+100; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	text := b.String()
	require.NoError(t, output.Write("stdout", []byte(text)))
	require.NoError(t, output.Finish())
	require.Len(t, *records, maxAgentOutputRecords+1, "the stored lines and the limit marker")

	limit := cutOf(output.Cuts(), "stdout", domain.OutputCutRecords)
	require.NotNil(t, limit)
	require.Equal(t, 1, limit.Count)
	require.EqualValues(t, maxAgentOutputRecords, limit.Limit)
	stored := 0
	for _, record := range (*records)[:maxAgentOutputRecords] {
		stored += len(record.content)
	}
	require.EqualValues(t, len(text)-stored, limit.Bytes, "every byte after the cap is counted")
	require.Nil(t, cutOf(output.Cuts(), "stdout", domain.OutputCutBytes))
}

// The 4 MiB cap.
func TestOutputCutsRecordTheByteCap(t *testing.T) {
	output, _ := collectAgentOutput(nil)
	line := strings.Repeat("y", 8<<10) + "\n"
	total := 0
	for total < maxCapturedLogBytes+64<<10 {
		require.NoError(t, output.Write("stdout", []byte(line)))
		total += len(line)
	}
	require.NoError(t, output.Finish())
	limit := cutOf(output.Cuts(), "stdout", domain.OutputCutBytes)
	require.NotNil(t, limit)
	require.EqualValues(t, maxCapturedLogBytes, limit.Limit)
	require.Greater(t, limit.Bytes, int64(32<<10))
	require.LessOrEqual(t, limit.Bytes, int64(total-maxCapturedLogBytes+len(line)))
}
