package ingress

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewDocsHandler_RejectsNonGetMethod(t *testing.T) {
	handler := NewDocsHandler()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/docs", nil)
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestNewDocsHandler_ReturnsSchemaWithoutTokenField(t *testing.T) {
	handler := NewDocsHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/docs", nil)
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var resp docsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Endpoint != "/api/ingress/hardware" || resp.Method != "POST" {
		t.Errorf("unexpected endpoint/method: %+v", resp)
	}

	// The auth mechanism is a header, never a body field -- a "token" key in
	// the schema would directly contradict resp.Auth and every integrator
	// who copies the schema verbatim would send an ignored field.
	if _, ok := resp.Schema["token"]; ok {
		t.Error("schema must not include a body-level token field; auth is header-only (X-API-Token)")
	}

	for _, field := range []string{"deviceId", "lat", "lng", "ax", "ay", "az", "ts"} {
		if _, ok := resp.Schema[field]; !ok {
			t.Errorf("schema missing expected field %q", field)
		}
	}
	if resp.Schema["ts"].Required {
		t.Error(`schema["ts"].Required = true, want false (timestamp is optional -- server stamps receipt time when omitted)`)
	}
	if !resp.Schema["deviceId"].Required {
		t.Error(`schema["deviceId"].Required = false, want true`)
	}
}

func TestNewDocsHandler_Esp32TemplateCompilesToValidJSONBodyAndAuthHeader(t *testing.T) {
	handler := NewDocsHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/docs", nil)
	rec := httptest.NewRecorder()

	handler(rec, req)

	var resp docsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Esp32Template == "" {
		t.Fatal("esp32_template is empty")
	}
	for _, want := range []string{
		`#include <WiFi.h>`,
		`#include <HTTPClient.h>`,
		`X-API-Token`,
		`http.POST(body)`,
		`\"deviceId\"`,
	} {
		if !strings.Contains(resp.Esp32Template, want) {
			t.Errorf("esp32_template missing expected substring %q", want)
		}
	}
	// The template's JSON body construction must never emit a "ts" key: this
	// sketch has no RTC/NTP wall-clock, and a fabricated millis()-based
	// timestamp would silently corrupt the backend's coincidence window.
	if strings.Contains(resp.Esp32Template, `\"ts\"`) {
		t.Error("esp32_template must not send a fabricated ts field without a real wall-clock source")
	}
}
