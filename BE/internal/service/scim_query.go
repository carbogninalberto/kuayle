package service

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/kuayle/kuayle-backend/internal/domain"
)

const SCIMMaxPage = 100

var scimEqualityFilter = regexp.MustCompile(`(?i)^\s*(userName|externalId|displayName|emails\.value|emails\[\s*type\s+eq\s+"work"\s*\]\.value)\s+eq\s+("(?:[^"\\]|\\.)*")\s*$`)

func ParseSCIMQuery(filter, start, count string, group bool) (domain.SCIMQuery, error) {
	query := domain.SCIMQuery{Start: 1, Count: SCIMMaxPage}
	if start != "" {
		value, err := strconv.Atoi(start)
		if err != nil {
			return query, scimInvalid("startIndex must be an integer")
		}
		query.Start = max(1, value)
	}
	if count != "" {
		value, err := strconv.Atoi(count)
		if err != nil {
			return query, scimInvalid("count must be an integer")
		}
		query.Count = min(SCIMMaxPage, max(0, value))
	}
	if filter == "" {
		return query, nil
	}
	invalid := &SCIMError{Type: "invalidFilter", Detail: "unsupported equality filter"}
	match := scimEqualityFilter.FindStringSubmatch(filter)
	if match == nil || json.Unmarshal([]byte(match[2]), &query.Value) != nil || !scimTextValid(query.Value) {
		return query, invalid
	}
	query.Attribute = strings.ToLower(match[1])
	if strings.HasPrefix(query.Attribute, "emails[") {
		query.Attribute = "workemail"
	}
	if group {
		if query.Attribute != "externalid" && query.Attribute != "displayname" {
			return query, invalid
		}
	} else if query.Attribute == "displayname" {
		return query, invalid
	}
	return query, nil
}
