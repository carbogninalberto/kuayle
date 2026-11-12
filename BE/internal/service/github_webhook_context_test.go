package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A delivery GitHub gave up on still records what Kuayle already received.
func TestAFinalSignalIsWrittenAfterTheDeliveryIsDropped(t *testing.T) {
	request, cancel := context.WithCancel(context.Background())
	cancel()
	before := time.Now()
	write, done := finalSignalContext(request)
	defer done()
	require.NoError(t, write.Err())
	deadline, ok := write.Deadline()
	require.True(t, ok, "the final write has its own short bound")
	require.False(t, deadline.Before(before.Add(signalWriteTimeout)))
	require.False(t, deadline.After(time.Now().Add(signalWriteTimeout)))
}
