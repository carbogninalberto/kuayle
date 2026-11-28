package repository

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func processApproval(t *testing.T, f skillFixture, actor uuid.UUID, chat uuid.UUID) uuid.UUID {
	t.Helper()
	var approval uuid.UUID
	err := f.db.Get(&approval, `WITH message AS (
 INSERT INTO conversation_messages(conversation_id,request_id,role,content,status)
 VALUES($1,gen_random_uuid(),'user','start the server','completed') RETURNING id
 ), call AS (
 INSERT INTO conversation_tool_calls(conversation_id,message_id,sequence,call_id,name,input,status)
 SELECT $1,id,0,id::text,'start_process','{}','running' FROM message RETURNING id
 )
 INSERT INTO conversation_approvals(conversation_id,tool_call_id,action,actor_id,payload,status,expires_at)
 SELECT $1,id,'start_process',$2,'{}','approving',now()+interval '1 hour' FROM call RETURNING id`, chat, actor)
	require.NoError(t, err)
	return approval
}

func TestAssistantProcessLifecycle(t *testing.T) {
	f := newSkillFixture(t)
	ctx := f.as(f.member, f.ws)
	chats := NewConversationRepository(f.db)
	store := NewDevMachineRepository(f.db)
	machineID := machineFor(t, f, f.member)
	machine, err := store.GetMachineInternal(ctx, machineID)
	require.NoError(t, err)
	terminal := runningTerminal(t, store, *machine)
	chat, err := chats.Create(ctx, f.ws, "Dev server")
	require.NoError(t, err)
	id := processApproval(t, f, f.member, chat.ID)
	payload := domain.AssistantProcessPayload{MachineID: machineID, Command: "python3 -m http.server \"$PORT\"", WorkingDirectory: "/workspace", Port: 4321}
	queue := func(id uuid.UUID, req domain.AssistantProcessPayload) error {
		return chats.QueueAssistantProcess(ctx, f.ws, chat.ID, id, req)
	}
	req := payload
	req.Port = terminal.InternalPort
	require.ErrorIs(t, queue(id, req), ErrProcessPortReserved)
	req.Port = 4322
	require.NoError(t, store.CreatePort(ctx, appPort(*machine, req.Port, domain.DevMachinePortSourceUser, nil)))
	require.ErrorIs(t, queue(id, req), ErrPortExists)
	require.NoError(t, queue(id, payload))
	require.NoError(t, queue(id, payload), "a retried approval creates one process")
	other := processApproval(t, f, f.member, chat.ID)
	require.ErrorIs(t, queue(other, payload), ErrProcessQuota)
	_, err = chats.AssistantProcess(f.as(f.admin, f.ws), f.ws, chat.ID, id, true)
	require.ErrorIs(t, err, ErrConversationNotFound, "workspace admins cannot stop another user's process")
	route := func() *domain.DevMachineService {
		t.Helper()
		_, service, err := store.GetRoute(ctx, machine.RoutingKey, domain.AppPortService(payload.Port))
		require.NoError(t, err)
		return service
	}
	require.Nil(t, route(), "a queued process has no live route")

	var op domain.DevMachineOperation
	require.NoError(t, f.db.Get(&op, `UPDATE dev_machine_operations SET status='leased',lease_owner='test',lease_expires_at=now()+interval '1 minute'
 WHERE idempotency_key=$1 RETURNING *`, "assistant-process:"+id.String()))
	p, err := store.AssistantProcessToStart(ctx, op)
	require.NoError(t, err)
	require.NotNil(t, p)
	require.Equal(t, payload.Command, p.Command)
	started, err := store.MarkAssistantProcessRunning(ctx, op)
	require.NoError(t, err)
	require.True(t, started)
	require.Equal(t, terminal.ID, route().ID, "launch uses the existing app port authentication")
	require.Equal(t, domain.AssistantProcessHost(id), route().InternalHost)
	require.NoError(t, store.SetListeningPorts(ctx, machineID, []int{payload.Port}, []int{payload.Port}))
	require.Equal(t, domain.AssistantProcessHost(id), route().InternalHost, "port scans cannot redirect a process URL")
	require.NoError(t, store.CompleteOperation(ctx, op.ID, "test"))

	host := fmt.Sprintf("%s-%d.machines.example.test", machine.RoutingKey, payload.Port)
	ticket := &domain.DevMachineAccessTicket{ID: uuid.New(), WorkspaceID: f.ws, MachineID: machineID, ServiceID: terminal.ID,
		UserID: f.member, BoundHost: host, TokenHash: strings.Repeat("a", 64), Status: domain.DevMachineAccessTicketStatusActive, ExpiresAt: time.Now().Add(time.Minute)}
	session := &domain.DevMachineAccessSession{ID: uuid.New(), WorkspaceID: f.ws, MachineID: machineID, ServiceID: terminal.ID,
		UserID: f.member, BoundHost: host, TokenHash: strings.Repeat("b", 64), ExpiresAt: time.Now().Add(time.Minute)}
	require.NoError(t, store.CreateAccessTicket(ctx, ticket))
	require.NoError(t, store.CreateAccessSession(ctx, session))
	stopped, err := chats.AssistantProcess(ctx, f.ws, chat.ID, id, true)
	require.NoError(t, err)
	require.Equal(t, "stopping", stopped.Status)
	require.Nil(t, route(), "stop immediately disables gateway access")
	require.ErrorIs(t, queue(other, payload), ErrProcessQuota, "cleanup still holds the reservation")
	require.NoError(t, store.FinishAssistantProcess(ctx, id, "stopped"))
	used, err := store.ConsumeAccessTicket(ctx, ticket.TokenHash, host)
	require.NoError(t, err)
	require.Nil(t, used)
	access, err := store.GetAccessSession(ctx, session.TokenHash, host)
	require.NoError(t, err)
	require.Nil(t, access)
	require.NoError(t, queue(other, payload), "cleanup frees the port and quota")
}

func TestAssistantProcessEndConditions(t *testing.T) {
	for _, condition := range []string{"expiry", "conversation deleted", "machine changed"} {
		t.Run(condition, func(t *testing.T) {
			f := newSkillFixture(t)
			ctx := f.as(f.member, f.ws)
			chats := NewConversationRepository(f.db)
			store := NewDevMachineRepository(f.db)
			machineID := machineFor(t, f, f.member)
			machine, err := store.GetMachineInternal(ctx, machineID)
			require.NoError(t, err)
			runningTerminal(t, store, *machine)
			chat, err := chats.Create(ctx, f.ws, "Dev server")
			require.NoError(t, err)
			id := processApproval(t, f, f.member, chat.ID)
			require.NoError(t, chats.QueueAssistantProcess(ctx, f.ws, chat.ID, id,
				domain.AssistantProcessPayload{MachineID: machineID, Command: "sleep 30", WorkingDirectory: "/workspace", Port: 4321}))
			_, err = f.db.Exec(`UPDATE assistant_processes SET status='running' WHERE id=$1`, id)
			require.NoError(t, err)
			switch condition {
			case "expiry":
				_, err = f.db.Exec(`UPDATE assistant_processes SET expires_at=now()-interval '1 second' WHERE id=$1`, id)
			case "conversation deleted":
				err = chats.Delete(ctx, f.ws, chat.ID)
			case "machine changed":
				_, err = f.db.Exec(`UPDATE dev_machines SET generation=generation+1 WHERE id=$1`, machineID)
			}
			require.NoError(t, err)
			_, route, err := store.GetRoute(ctx, machine.RoutingKey, domain.AppPortService(4321))
			require.NoError(t, err)
			require.Nil(t, route, "the old process is unreachable even before cleanup")
			processes, err := store.AssistantProcessesToCheck(ctx)
			require.NoError(t, err)
			require.True(t, slices.ContainsFunc(processes, func(process domain.AssistantProcess) bool {
				return process.ID == id && process.Status == "stopping"
			}), "the manager will remove the process")
		})
	}
}

func TestAssistantProcessWorkspaceQuotaSerializesApprovals(t *testing.T) {
	f := newSkillFixture(t)
	chats := NewConversationRepository(f.db)
	_, err := f.db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'member')`, f.ws, f.outsider)
	require.NoError(t, err)
	users := []uuid.UUID{f.owner, f.admin, f.member, f.teamMember, f.outsider}
	results := make(chan error, len(users))
	start := make(chan struct{})
	for _, user := range users {
		ctx := f.as(user, f.ws)
		machine := machineFor(t, f, user)
		chat, err := chats.Create(ctx, f.ws, "Dev server")
		require.NoError(t, err)
		approval := processApproval(t, f, user, chat.ID)
		go func() {
			<-start
			results <- chats.QueueAssistantProcess(ctx, f.ws, chat.ID, approval,
				domain.AssistantProcessPayload{MachineID: machine, Command: "sleep 30", WorkingDirectory: "/workspace", Port: 4321})
		}()
	}
	close(start)
	denied := 0
	for range users {
		if err := <-results; err != nil {
			require.ErrorIs(t, err, ErrProcessQuota)
			denied++
		}
	}
	require.Equal(t, 1, denied)
}
