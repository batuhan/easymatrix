package server

import (
	"strings"

	"maunium.net/go/mautrix/event"

	"github.com/batuhan/easymatrix/internal/compat"
)

type userShape struct {
	ID            string
	Username      string
	PhoneNumber   string
	Email         string
	FullName      string
	ImgURL        string
	CannotMessage bool
	IsSelf        bool
}

func newCompatUser(shape userShape) compat.User {
	userID := strings.TrimSpace(shape.ID)
	fullName := strings.TrimSpace(shape.FullName)
	if fullName == "" {
		fullName = userID
	}
	return compat.User{
		ID:            userID,
		Username:      strings.TrimSpace(shape.Username),
		PhoneNumber:   strings.TrimSpace(shape.PhoneNumber),
		Email:         strings.TrimSpace(shape.Email),
		FullName:      fullName,
		ImgURL:        strings.TrimSpace(shape.ImgURL),
		CannotMessage: shape.CannotMessage,
		IsSelf:        shape.IsSelf,
	}
}

func userFromLocalBridgeProfile(remoteID string, profileData map[string]any) compat.User {
	return newCompatUser(userShape{
		ID:            remoteID,
		Username:      firstString(profileData, "username", "handle"),
		PhoneNumber:   firstString(profileData, "phone", "phone_number"),
		Email:         firstString(profileData, "email"),
		FullName:      firstString(profileData, "name", "display_name", "displayName"),
		ImgURL:        firstString(profileData, "avatar", "avatar_url"),
		CannotMessage: false,
		IsSelf:        true,
	})
}

func userFromMemberEvent(userID string, member event.MemberEventContent, selfUserID string) compat.User {
	return newCompatUser(userShape{
		ID:            userID,
		FullName:      member.Displayname,
		ImgURL:        string(member.AvatarURL),
		CannotMessage: false,
		IsSelf:        userID == selfUserID,
	})
}
