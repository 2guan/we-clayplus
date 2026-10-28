package chatroom

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestDisplayNameDoesNotRequireLegacyAbilityImplementations(t *testing.T) {
	type legacyAbility struct{ Ability }
	var old Ability = legacyAbility{}
	_, err := (Server{Impl: old}).SetDisplayName(context.Background(), &SetDisplayName_Request{Chatroom: "1@chatroom", DisplayName: "test"})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("old ability should remain valid and report unsupported extension: %v", err)
	}
}
