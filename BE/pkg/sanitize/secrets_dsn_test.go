package sanitize

import "testing"

func TestRedactDSNsMasksURLPasswordsAndKeyValueForms(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"postgres URL", "postgres://app:hunter2@db:5432/app?sslmode=require", "postgres://app:[REDACTED]@db:5432/app?sslmode=require"},
		{"postgres URL without a password", "postgres://app@host/db", "postgres://app@host/db"},
		{"postgres URL without a user", "postgres://:hunter2@db:5432/app", "postgres://:[REDACTED]@db:5432/app"},
		{"mysql URL", "mysql://u:p@h/db", "mysql://u:[REDACTED]@h/db"},
		{"URL without credentials", "https://example.com/path?q=1", "https://example.com/path?q=1"},
		{"key/value DSN", "host=db user=app password=hunter2 dbname=app", "host=db user=app password=[REDACTED] dbname=app"},
		{"PGPASSWORD", "PGPASSWORD=hunter2 ./migrate up", "PGPASSWORD=[REDACTED] ./migrate up"},
		{"exported DB_PASSWORD", "export DB_PASSWORD=hunter2; ./migrate", "export DB_PASSWORD=[REDACTED] ./migrate"},
		{"source_password", "SOURCE_PASSWORD=hunter2", "SOURCE_PASSWORD=[REDACTED]"},
		{"PWD untouched", "cd /workspace; PWD=/workspace", "cd /workspace; PWD=/workspace"},
		{"empty value untouched", "password= ./migrate", "password= ./migrate"},
		{"plain text", "Run `make check` before pushing.", "Run `make check` before pushing."},
	}
	for _, c := range cases {
		if got := RedactDSNs(c.in); got != c.want {
			t.Errorf("%s: RedactDSNs(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}
