package service

import (
	"encoding/json"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

type scimPatchOperation struct {
	Op    string          `json:"op"`
	Path  string          `json:"path"`
	Value json.RawMessage `json:"value"`
}

type scimPatch struct {
	Operations []scimPatchOperation `json:"Operations"`
}

var (
	scimEmailPath  = regexp.MustCompile(`(?i)^emails\[\s*type\s+eq\s+("(?:[^"\\]|\\.)*")\s*\]\.value$`)
	scimMemberPath = regexp.MustCompile(`(?i)^members\[\s*value\s+eq\s+"([0-9a-f-]+)"\s*\]$`)
)

func applySCIMPatch(body json.RawMessage, apply func(scimPatchOperation) error) error {
	var patch scimPatch
	if err := decodeSCIMResource(body, SCIMMessage+"PatchOp", &patch); err != nil {
		return err
	}
	if len(patch.Operations) == 0 {
		return scimInvalid("Operations must contain at least one operation")
	}
	for _, op := range patch.Operations {
		op.Op = strings.ToLower(op.Op)
		switch op.Op {
		case "add", "replace":
			if len(op.Value) == 0 {
				return scimInvalid("add and replace require a value")
			}
		case "remove":
			if op.Path == "" {
				return &SCIMError{Type: "noTarget", Detail: "remove requires a path"}
			}
		default:
			return &SCIMError{Type: "invalidSyntax", Detail: "invalid PATCH operation"}
		}
		if op.Path != "" {
			if err := apply(op); err != nil {
				return err
			}
			continue
		}
		var values map[string]json.RawMessage
		if err := json.Unmarshal(op.Value, &values); err != nil || values == nil {
			return scimInvalid("PATCH without a path requires an object value")
		}
		for _, key := range slices.Sorted(maps.Keys(values)) {
			switch strings.ToLower(key) {
			case "id", "meta", "schemas":
				continue
			}
			if err := apply(scimPatchOperation{Op: op.Op, Path: key, Value: values[key]}); err != nil {
				return err
			}
		}
	}
	return nil
}

func patchSCIMUser(user *domain.SCIMUser, body json.RawMessage) error {
	return applySCIMPatch(body, func(op scimPatchOperation) error {
		switch strings.ToLower(stripSCIMSchema(op.Path, SCIMUserSchema)) {
		case "username":
			return patchSCIMValue(&user.UserName, op)
		case "externalid":
			return patchSCIMValue(&user.ExternalID, op)
		case "displayname":
			return patchSCIMValue(&user.DisplayName, op)
		case "name":
			return patchSCIMValue(&user.Name, op)
		case "name.formatted":
			return patchSCIMValue(&user.Name.Formatted, op)
		case "name.familyname":
			return patchSCIMValue(&user.Name.FamilyName, op)
		case "name.givenname":
			return patchSCIMValue(&user.Name.GivenName, op)
		case "name.middlename":
			return patchSCIMValue(&user.Name.MiddleName, op)
		case "active":
			var active scimBoolean
			if op.Op == "remove" || json.Unmarshal(op.Value, &active) != nil {
				return scimInvalid("active must be set to true or false")
			}
			user.Active = bool(active)
			return nil
		case "emails":
			if op.Op != "add" {
				return patchSCIMValue(&user.Emails, op)
			}
			var added []domain.SCIMEmail
			if err := patchSCIMValue(&added, op); err != nil {
				return err
			}
			user.Emails = append(user.Emails, added...)
			return nil
		default:
			if match := scimEmailPath.FindStringSubmatch(stripSCIMSchema(op.Path, SCIMUserSchema)); match != nil {
				return patchSCIMTypedEmail(user, match[1], op)
			}
			return invalidSCIMPath(op.Path)
		}
	})
}

func patchSCIMTypedEmail(user *domain.SCIMUser, quotedType string, op scimPatchOperation) error {
	var emailType string
	if err := json.Unmarshal([]byte(quotedType), &emailType); err != nil {
		return invalidSCIMPath(op.Path)
	}
	if op.Op == "remove" {
		user.Emails = slices.DeleteFunc(user.Emails, func(email domain.SCIMEmail) bool {
			return strings.EqualFold(email.Type, emailType)
		})
		return nil
	}
	var value string
	if err := patchSCIMValue(&value, op); err != nil {
		return err
	}
	for i := range user.Emails {
		if strings.EqualFold(user.Emails[i].Type, emailType) {
			user.Emails[i].Value = value
			return nil
		}
	}
	user.Emails = append(user.Emails, domain.SCIMEmail{Value: value, Type: emailType})
	return nil
}

func patchSCIMGroup(group *domain.SCIMGroup, body json.RawMessage) error {
	return applySCIMPatch(body, func(op scimPatchOperation) error {
		path := stripSCIMSchema(op.Path, SCIMGroupSchema)
		switch strings.ToLower(path) {
		case "displayname":
			return patchSCIMValue(&group.DisplayName, op)
		case "externalid":
			return patchSCIMValue(&group.ExternalID, op)
		case "members":
			return patchSCIMMembers(group, op)
		default:
			if match := scimMemberPath.FindStringSubmatch(path); match != nil && op.Op == "remove" {
				id, err := uuid.Parse(match[1])
				if err == nil {
					group.Members = slices.DeleteFunc(group.Members, func(member domain.SCIMGroupMember) bool {
						return member.Value == id
					})
					return nil
				}
			}
			return invalidSCIMPath(op.Path)
		}
	})
}

func patchSCIMMembers(group *domain.SCIMGroup, op scimPatchOperation) error {
	if op.Op == "remove" && len(op.Value) == 0 {
		group.Members = nil
		return nil
	}
	var members []domain.SCIMGroupMember
	if err := json.Unmarshal(op.Value, &members); err != nil {
		return scimInvalid("members must be a list of user references")
	}
	switch op.Op {
	case "add":
		group.Members = append(group.Members, members...)
	case "replace":
		group.Members = members
	case "remove":
		// Entra also sends a members array with remove and no value filter.
		removed := make(map[uuid.UUID]bool, len(members))
		for _, member := range members {
			removed[member.Value] = true
		}
		group.Members = slices.DeleteFunc(group.Members, func(member domain.SCIMGroupMember) bool {
			return removed[member.Value]
		})
	}
	return nil
}

func patchSCIMValue[T any](target *T, op scimPatchOperation) error {
	if op.Op == "remove" || string(op.Value) == "null" {
		var zero T
		*target = zero
		return nil
	}
	if err := json.Unmarshal(op.Value, target); err != nil {
		return scimInvalid("invalid PATCH value for " + op.Path)
	}
	return nil
}

func stripSCIMSchema(path, schema string) string {
	if len(path) > len(schema) && path[len(schema)] == ':' && strings.EqualFold(path[:len(schema)], schema) {
		return path[len(schema)+1:]
	}
	return path
}

func invalidSCIMPath(path string) error {
	switch strings.ToLower(path) {
	case "id", "meta", "schemas":
		return &SCIMError{Type: "mutability", Detail: "attribute is read-only"}
	}
	return &SCIMError{Type: "invalidPath", Detail: "unsupported PATCH path"}
}
