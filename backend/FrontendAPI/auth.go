package FrontendAPI

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"unicode"

	"GO/rag"
)

var ErrUnauthenticated = errors.New("unauthenticated")
var ErrForbidden = errors.New("forbidden")

// Authenticator can use an existing backend session, middleware context or token store.
// Never construct its result from request JSON or unverified role/team headers.
type Authenticator func(*http.Request) (rag.Access, error)

func validAccess(access rag.Access) bool {
	switch access.Role {
	case "intern", "buddy", "manager":
		return access.Team == "" || strings.TrimSpace(access.Team) != ""
	default:
		return false
	}
}

// NewTokenAuthenticator creates a demo token store. Copying the input protects
// against accidental permission changes or concurrent mutations by the caller.
// Production can supply an Authenticator backed by the existing identity system.
func NewTokenAuthenticator(users map[string]rag.Access) (Authenticator, error) {
	if len(users) == 0 {
		return nil, errors.New("at least one demo user is required")
	}
	lookup := make(map[[32]byte]rag.Access, len(users))
	for token, access := range users {
		if len(token) < 32 || strings.IndexFunc(token, unicode.IsSpace) >= 0 {
			return nil, errors.New("demo tokens must contain at least 32 characters and no whitespace")
		}
		if !validAccess(access) {
			return nil, errors.New("demo user has invalid role or team")
		}
		lookup[sha256.Sum256([]byte(token))] = access
	}
	return func(r *http.Request) (rag.Access, error) {
		values := r.Header.Values("Authorization")
		if len(values) != 1 {
			return rag.Access{}, ErrUnauthenticated
		}
		parts := strings.Fields(values[0])
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			return rag.Access{}, ErrUnauthenticated
		}
		access, ok := lookup[sha256.Sum256([]byte(parts[1]))]
		if !ok {
			return rag.Access{}, ErrUnauthenticated
		}
		return access, nil
	}, nil
}

// DemoAuthenticatorFromEnv has no default credentials and fails closed.
// HTTP_DEMO_USERS is a JSON object: {"opaque-token":{"role":"intern","team":"backend"}}.
func DemoAuthenticatorFromEnv() (Authenticator, error) {
	decoder := json.NewDecoder(strings.NewReader(os.Getenv("HTTP_DEMO_USERS")))
	decoder.DisallowUnknownFields()
	var users map[string]struct {
		Role string `json:"role"`
		Team string `json:"team"`
	}
	if err := decoder.Decode(&users); err != nil {
		// Do not include JSON input in errors: the keys are credentials.
		return nil, errors.New("HTTP_DEMO_USERS must contain a valid JSON user map")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, errors.New("HTTP_DEMO_USERS contains trailing data")
	}
	access := make(map[string]rag.Access, len(users))
	for token, user := range users {
		access[token] = rag.Access{Role: user.Role, Team: user.Team}
	}
	authenticate, err := NewTokenAuthenticator(access)
	if err != nil {
		return nil, fmt.Errorf("HTTP_DEMO_USERS: %w", err)
	}
	return authenticate, nil
}
