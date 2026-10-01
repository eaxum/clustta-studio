package main

import (
	"clustta/internal/compatibility"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNegotiateRequestAPIDefaultsLegacyClients(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/project/data", nil)
	response := httptest.NewRecorder()

	negotiated, ok := negotiateRequestAPI(response, request)
	if !ok {
		t.Fatal("legacy request rejected")
	}
	api := compatibility.FromContext(negotiated.Context())
	if api.Version != compatibility.LegacyAPIVersion {
		t.Fatalf("expected API %s, got %s", compatibility.LegacyAPIVersion, api.Version)
	}
}

func TestNegotiateRequestAPIAcceptsCurrentClient(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/project/data", nil)
	request.Header.Set(compatibility.APIVersionHeader, compatibility.CurrentAPIVersion)
	response := httptest.NewRecorder()

	negotiated, ok := negotiateRequestAPI(response, request)
	if !ok {
		t.Fatal("current request rejected")
	}
	api := compatibility.FromContext(negotiated.Context())
	if api.Version != compatibility.CurrentAPIVersion {
		t.Fatalf("expected API %s, got %s", compatibility.CurrentAPIVersion, api.Version)
	}
}

func TestNegotiateRequestAPIRejectsUnsupportedVersion(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/project/data", nil)
	request.Header.Set(compatibility.APIVersionHeader, "3")
	response := httptest.NewRecorder()

	if _, ok := negotiateRequestAPI(response, request); ok {
		t.Fatal("unsupported request admitted")
	}
	if response.Code != http.StatusUpgradeRequired {
		t.Fatalf("expected HTTP 426, got %d", response.Code)
	}
}
