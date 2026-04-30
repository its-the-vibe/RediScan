package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPrettyPrintJSON_ValidJSON(t *testing.T) {
	input := `{"name":"Alice","age":30}`
	result := prettyPrintJSON(input)
	if !strings.Contains(result, "\n") {
		t.Errorf("expected pretty-printed JSON with newlines, got: %s", result)
	}
	if !strings.Contains(result, `"name"`) {
		t.Errorf("expected key 'name' in output, got: %s", result)
	}
}

func TestPrettyPrintJSON_InvalidJSON(t *testing.T) {
	input := "not json at all"
	result := prettyPrintJSON(input)
	if result != input {
		t.Errorf("expected original string for invalid JSON, got: %s", result)
	}
}

func TestPrettyPrintJSON_EmptyString(t *testing.T) {
	result := prettyPrintJSON("")
	// empty string is not valid JSON; should be returned as-is
	if result != "" {
		t.Errorf("expected empty string for empty input, got: %s", result)
	}
}

func TestPrettyPrintJSON_JSONArray(t *testing.T) {
	input := `[1,2,3]`
	result := prettyPrintJSON(input)
	if !strings.Contains(result, "\n") {
		t.Errorf("expected pretty-printed JSON array with newlines, got: %s", result)
	}
}

func TestRenderNotFound(t *testing.T) {
	rr := httptest.NewRecorder()
	renderNotFound(rr, "Key 'test' does not exist")

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "404") {
		t.Errorf("expected '404' in response body, got: %s", body)
	}
	if !strings.Contains(body, "Key &#39;test&#39; does not exist") && !strings.Contains(body, "Key 'test' does not exist") {
		t.Errorf("expected message in response body, got: %s", body)
	}
}

func TestRenderError(t *testing.T) {
	rr := httptest.NewRecorder()
	renderError(rr, "something went wrong")

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Error") {
		t.Errorf("expected 'Error' in response body, got: %s", body)
	}
	if !strings.Contains(body, "something went wrong") {
		t.Errorf("expected error message in response body, got: %s", body)
	}
}

func TestIndexHandler_NotFound(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	rr := httptest.NewRecorder()

	indexHandler(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected status 404 for non-root path, got %d", rr.Code)
	}
}

func TestLindexHandler_MissingKey(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/lindex", nil)
	rr := httptest.NewRecorder()

	lindexHandler(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected status 404 for missing key, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Missing") {
		t.Errorf("expected 'Missing' in response body, got: %s", body)
	}
}
