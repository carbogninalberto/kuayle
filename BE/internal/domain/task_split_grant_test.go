package domain

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The creator may grant the lead the split (KUA-313): a grant names the
// lead and no check, on a pipeline that has a lead.
func TestTaskRoomCreatorGrantsTheSplit(t *testing.T) {
	grant := TaskRoomPost{AuthorKind: RoomAuthorHuman, Kind: RoomGrant, Data: TaskRoomData{Role: PipelineLeadRole, Action: RoomRequestSplit}}
	require.Empty(t, TaskRoomRefusal(roomPipeline(true), grant, false))
	require.NotEmpty(t, TaskRoomRefusal(roomPipeline(false), grant, false), "no lead to grant it to")
	withCheck := grant
	withCheck.Data.Check = "ci"
	require.NotEmpty(t, TaskRoomRefusal(roomPipeline(true), withCheck, false), "a split grant names no check")
	for _, role := range []string{"implementer", "reviewer", PipelineLeadRole} {
		agent := grant
		agent.AuthorKind, agent.AuthorRole = RoomAuthorMember, role
		require.NotEmpty(t, TaskRoomRefusal(roomPipeline(true), agent, true), role)
	}
}

// The lead reads a split grant as one: it may split without asking.
func TestTheLeadReadsASplitGrant(t *testing.T) {
	line := roomDataLine(TaskMessage{Kind: RoomGrant, Data: TaskRoomData{Role: PipelineLeadRole, Action: RoomRequestSplit}})
	require.Equal(t, "lead may split the issue without approval", line)
}
