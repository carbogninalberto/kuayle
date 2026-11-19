package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestStoppedPublicationToolRefusesGuestsBeforeLookingUpWork(t *testing.T) {
	s := &ConversationService{tools: &AssistantTools{members: memberFake{domain.RoleGuest}}}
	chat := domain.Conversation{WorkspaceID: uuid.New(), CreatorID: uuid.New()}
	_, _, err := s.prepareStoppedPublication(context.Background(), chat, `{"identifier":"ENG-1"}`)
	require.Contains(t, toolErrorText(err), "Ask one of them to request and approve publication")
}
