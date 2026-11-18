package service

import (
	"context"
	"encoding/json"

	"github.com/kuayle/kuayle-backend/internal/importer"
	"github.com/kuayle/kuayle-backend/internal/importer/jira"
)

type jiraImportConfig struct {
	Cloud bool   `json:"cloud"`
	JQL   string `json:"jql"`
}

type jiraImportCredentials struct {
	BaseURL     string `json:"base_url"`
	Email       string `json:"email"`
	Token       string `json:"token"`
	BearerToken string `json:"bearer_token"`
}

// JiraSourceFactory builds a Jira adapter. Credentials are decrypted by the
// service and live only in the client used by this job; they are never stored
// in the source or logged.
func JiraSourceFactory(allowPrivate bool, attachmentLimit int64) SourceFactory {
	return func(_ context.Context, config, credentials json.RawMessage) (importer.Source, error) {
		var cfg jiraImportConfig
		if len(config) > 0 {
			if err := json.Unmarshal(config, &cfg); err != nil {
				return nil, err
			}
		}
		var creds jiraImportCredentials
		if err := json.Unmarshal(credentials, &creds); err != nil {
			return nil, err
		}
		client, err := jira.NewClient(jira.Config{
			BaseURL:             creds.BaseURL,
			Email:               creds.Email,
			Token:               creds.Token,
			BearerToken:         creds.BearerToken,
			Cloud:               cfg.Cloud,
			AllowPrivateNetwork: allowPrivate,
			MaxAttachmentSize:   attachmentLimit,
		})
		if err != nil {
			return nil, err
		}
		return client.WithJQL(cfg.JQL), nil
	}
}
