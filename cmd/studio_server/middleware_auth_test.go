package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestApiTokenMiddlewareRejectsHeaderOnlyIdentity(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})
	handler := sessionManager.LoadAndSave(ApiTokenMiddleware(next))
	request := httptest.NewRequest(http.MethodGet, "/studio/persons", nil)
	request.Header.Set("UserData", `{"id":"admin"}`)
	request.Header.Set("UserId", "admin")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
	if called {
		t.Fatal("protected handler was called")
	}
}

func TestApiTokenMiddlewareAllowsPreflight(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})
	handler := ApiTokenMiddleware(next)
	request := httptest.NewRequest(http.MethodOptions, "/studio/persons", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if !called {
		t.Fatal("preflight request was rejected")
	}
}
