package dto_test

import (
	"strings"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/pkg/validate"
	"github.com/stretchr/testify/require"
)

func ptr(s string) *string { return &s }

func TestProjectColorIconValidation(t *testing.T) {
	tests := []struct {
		name    string
		color   *string
		icon    *string
		wantErr bool
	}{
		{name: "absent", wantErr: false},
		{name: "hex colour and lucide icon", color: ptr("#3b82f6"), icon: ptr("Rocket"), wantErr: false},
		{name: "emoji icon", icon: ptr("emoji:🚀"), wantErr: false},
		{name: "empty clears", color: ptr(""), icon: ptr(""), wantErr: false},
		{name: "colour name", color: ptr("blue"), wantErr: true},
		{name: "colour without hash", color: ptr("3b82f6"), wantErr: true},
		{name: "icon too long", icon: ptr(strings.Repeat("a", 51)), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			create := dto.CreateProjectRequest{Name: "P", Color: tt.color, Icon: tt.icon}
			update := dto.UpdateProjectRequest{Color: tt.color, Icon: tt.icon}
			if tt.wantErr {
				require.Error(t, validate.Struct(create))
				require.Error(t, validate.Struct(update))
				return
			}
			require.NoError(t, validate.Struct(create))
			require.NoError(t, validate.Struct(update))
		})
	}
}
