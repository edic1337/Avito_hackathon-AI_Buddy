package FrontendAPI

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"GO/rag"
)

func TestTokenIdentityAndSnapshot(t *testing.T) {
	users := map[string]rag.Access{
		testToken:               {Role: "intern", Team: "backend"},
		strings.Repeat("b", 32): {Role: "buddy", Team: "backend"},
		strings.Repeat("m", 32): {Role: "manager", Team: "other"},
	}
	auth, err := NewTokenAuthenticator(users)
	if err != nil {
		t.Fatal(err)
	}
	for token, want := range users {
		r := request(`{}`)
		r.Header.Set("Authorization", "bearer "+token)
		got, err := auth(r)
		if err != nil || got != want {
			t.Fatal(got, err)
		}
	}
	users[testToken] = rag.Access{Role: "manager"}
	delete(users, strings.Repeat("m", 32))
	got, err := auth(request(`{}`))
	if err != nil || got.Role != "intern" || got.Team != "backend" {
		t.Fatal("caller changed permissions", got, err)
	}
	r := request(`{}`)
	r.Header.Set("Authorization", "Bearer unknown")
	if _, err = auth(r); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal(err)
	}
}

func TestDemoAuthConfiguration(t *testing.T) {
	for _, users := range []map[string]rag.Access{
		nil, {}, {"short": {Role: "intern"}},
		{strings.Repeat(" ", 32): {Role: "intern"}},
		{testToken: {Role: "admin"}}, {testToken: {Role: "intern", Team: " "}},
	} {
		if _, err := NewTokenAuthenticator(users); err == nil {
			t.Fatal("invalid user configuration accepted")
		}
	}
	for _, raw := range []string{"", "null", "{}", "[]", "{", `{"` + testToken + `":{"role":"intern","typo":"backend"}}`, `{} {}`} {
		t.Setenv("HTTP_DEMO_USERS", raw)
		if _, err := DemoAuthenticatorFromEnv(); err == nil || strings.Contains(err.Error(), testToken) {
			t.Fatal("configuration accepted or token leaked", err)
		}
	}
	raw, _ := json.Marshal(map[string]any{testToken: map[string]string{"role": "intern", "team": "backend"}})
	t.Setenv("HTTP_DEMO_USERS", string(raw))
	auth, err := DemoAuthenticatorFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	access, err := auth(request(`{}`))
	if err != nil || access != (rag.Access{Role: "intern", Team: "backend"}) {
		t.Fatal(access, err)
	}
}
