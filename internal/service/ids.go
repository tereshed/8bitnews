package service

import (
	"strings"

	"github.com/google/uuid"
)

// parseID turns an id an agent sent into its canonical uuid form, or an
// invalid-argument error: every id column is a uuid, and anything else would
// reach Postgres and come back as a cryptic internal error.
func parseID(field, v string) (string, error) {
	u, err := uuid.Parse(strings.TrimSpace(v))
	if err != nil {
		return "", invalidf("%s %q is not an id", field, v)
	}
	return u.String(), nil
}
