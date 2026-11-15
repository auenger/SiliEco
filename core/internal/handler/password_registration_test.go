package handler

import (
	"fmt"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPasswordRegistrationWithoutEmailCode(t *testing.T) {
	email := fmt.Sprintf("directory-preview-%d@example.test", time.Now().UnixNano())
	payload := map[string]any{"email": email, "password": "test-password-unique-123"}
	w := httptest.NewRecorder()
	testHandler.RegisterPassword(w, newRequest("POST", "/auth/password/register", payload))
	if w.Code != 200 {
		t.Fatalf("register: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	testHandler.LoginPassword(w, newRequest("POST", "/auth/password/login", payload))
	if w.Code != 200 {
		t.Fatalf("login: %d", w.Code)
	}
	w = httptest.NewRecorder()
	testHandler.LoginPassword(w, newRequest("POST", "/auth/password/login", map[string]any{"email": email, "password": "wrong-password"}))
	if w.Code != 401 {
		t.Fatalf("wrong password: %d", w.Code)
	}
}
