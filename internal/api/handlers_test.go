package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
)

// MockKVStore implements KVStore interface for testing
type MockKVStore struct {
	data map[string]string
}

func NewMockKVStore() *MockKVStore {
	return &MockKVStore{
		data: make(map[string]string),
	}
}

func (m *MockKVStore) Get(key string) (string, error) {
	if value, exists := m.data[key]; exists {
		return value, nil
	}
	return "", &KeyNotFoundError{Key: key}
}

func (m *MockKVStore) Put(key, value string) error {
	m.data[key] = value
	return nil
}

func (m *MockKVStore) Delete(key string) error {
	if _, exists := m.data[key]; !exists {
		return &KeyNotFoundError{Key: key}
	}
	delete(m.data, key)
	return nil
}

func (m *MockKVStore) Close() error {
	return nil
}

// KeyNotFoundError represents a key not found error
type KeyNotFoundError struct {
	Key string
}

func (e *KeyNotFoundError) Error() string {
	return "key not found"
}

// setupTestServer creates a test server with mock store
func setupTestServer() *Server {
	store := NewMockKVStore()
	return NewServer(store)
}

// executeRequest executes a request and returns the response recorder
func executeRequest(server *Server, req *http.Request) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, req)
	return rr
}

// TestHandlePutJSONValidation tests JSON validation for PUT requests
func TestHandlePutJSONValidation(t *testing.T) {
	server := setupTestServer()

	tests := []struct {
		name           string
		key            string
		body           string
		contentType    string
		expectedStatus int
		expectedError  string
		expectedCode   string
	}{
		{
			name:           "Valid JSON",
			key:            "test-key",
			body:           `{"value": "test-value"}`,
			contentType:    "application/json",
			expectedStatus: http.StatusCreated,
		},
		{
			name:           "Invalid JSON - malformed",
			key:            "test-key",
			body:           `{"value": "test-value"`,
			contentType:    "application/json",
			expectedStatus: http.StatusBadRequest,
			expectedError:  "Invalid JSON format",
			expectedCode:   "INVALID_JSON",
		},
		{
			name:           "Invalid JSON - empty object",
			key:            "test-key",
			body:           `{}`,
			contentType:    "application/json",
			expectedStatus: http.StatusBadRequest,
			expectedError:  "Value is required and cannot be empty",
			expectedCode:   "EMPTY_VALUE",
		},
		{
			name:           "Invalid JSON - null value",
			key:            "test-key",
			body:           `{"value": null}`,
			contentType:    "application/json",
			expectedStatus: http.StatusBadRequest,
			expectedError:  "Value is required and cannot be empty",
			expectedCode:   "EMPTY_VALUE",
		},
		{
			name:           "Invalid JSON - empty string value",
			key:            "test-key",
			body:           `{"value": ""}`,
			contentType:    "application/json",
			expectedStatus: http.StatusBadRequest,
			expectedError:  "Value is required and cannot be empty",
			expectedCode:   "EMPTY_VALUE",
		},
		{
			name:           "Invalid content type",
			key:            "test-key",
			body:           `{"value": "test-value"}`,
			contentType:    "text/plain",
			expectedStatus: http.StatusBadRequest,
			expectedError:  "Content-Type must be application/json",
			expectedCode:   "INVALID_CONTENT_TYPE",
		},
		{
			name:           "Empty body",
			key:            "test-key",
			body:           "",
			contentType:    "application/json",
			expectedStatus: http.StatusBadRequest,
			expectedError:  "Request body is required",
			expectedCode:   "EMPTY_BODY",
		},
		{
			name:           "Invalid JSON - wrong field name",
			key:            "test-key",
			body:           `{"val": "test-value"}`,
			contentType:    "application/json",
			expectedStatus: http.StatusBadRequest,
			expectedError:  "Value is required and cannot be empty",
			expectedCode:   "EMPTY_VALUE",
		},
		{
			name:           "Invalid JSON - extra fields (should work)",
			key:            "test-key",
			body:           `{"value": "test-value", "extra": "field"}`,
			contentType:    "application/json",
			expectedStatus: http.StatusCreated,
		},
		{
			name:           "Large JSON payload",
			key:            "test-key",
			body:           `{"value": "` + strings.Repeat("a", 1000) + `"}`,
			contentType:    "application/json",
			expectedStatus: http.StatusCreated,
		},
		{
			name:           "JSON with special characters",
			key:            "test-key",
			body:           `{"value": "test\nvalue\twith\rspecial\"chars"}`,
			contentType:    "application/json",
			expectedStatus: http.StatusCreated,
		},
		{
			name:           "Missing key in URL",
			key:            "",
			body:           `{"value": "test-value"}`,
			contentType:    "application/json",
			expectedStatus: http.StatusNotFound, // 404 because route doesn't match
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req *http.Request
			if tt.body != "" {
				req = httptest.NewRequest("PUT", "/kv/"+tt.key, bytes.NewBufferString(tt.body))
			} else {
				req = httptest.NewRequest("PUT", "/kv/"+tt.key, nil)
			}
			
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}

			// Set up URL vars for mux
			req = mux.SetURLVars(req, map[string]string{"key": tt.key})

			rr := executeRequest(server, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, rr.Code)
			}

			if tt.expectedError != "" {
				var response map[string]interface{}
				err := json.Unmarshal(rr.Body.Bytes(), &response)
				if err != nil {
					t.Errorf("Failed to unmarshal error response: %v", err)
				}

				if response["error"] != tt.expectedError {
					t.Errorf("Expected error '%s', got '%s'", tt.expectedError, response["error"])
				}

				if tt.expectedCode != "" && response["code"] != tt.expectedCode {
					t.Errorf("Expected error code '%s', got '%s'", tt.expectedCode, response["code"])
				}
			}

			// For successful requests, verify response structure
			if rr.Code == http.StatusCreated {
				var response map[string]interface{}
				err := json.Unmarshal(rr.Body.Bytes(), &response)
				if err != nil {
					t.Errorf("Failed to unmarshal success response: %v", err)
				}

				if response["key"] != tt.key {
					t.Errorf("Expected key '%s' in response, got '%s'", tt.key, response["key"])
				}

				if response["message"] == "" {
					t.Error("Expected success message in response")
				}
			}
		})
	}
}

// TestHandleGetValidation tests GET request validation
func TestHandleGetValidation(t *testing.T) {
	server := setupTestServer()
	store := server.store.(*MockKVStore)
	
	// Set up test data
	store.Put("existing-key", "existing-value")

	tests := []struct {
		name           string
		key            string
		expectedStatus int
		expectedError  string
		expectedCode   string
	}{
		{
			name:           "Valid key exists",
			key:            "existing-key",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Valid key not found",
			key:            "nonexistent-key",
			expectedStatus: http.StatusNotFound,
			expectedError:  "Key not found",
			expectedCode:   "KEY_NOT_FOUND",
		},
		{
			name:           "Empty key",
			key:            "",
			expectedStatus: http.StatusNotFound, // 404 because route doesn't match
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/kv/"+tt.key, nil)
			req = mux.SetURLVars(req, map[string]string{"key": tt.key})

			rr := executeRequest(server, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, rr.Code)
			}

			if tt.expectedError != "" {
				var response map[string]interface{}
				err := json.Unmarshal(rr.Body.Bytes(), &response)
				if err != nil {
					t.Errorf("Failed to unmarshal error response: %v", err)
				}

				if response["error"] != tt.expectedError {
					t.Errorf("Expected error '%s', got '%s'", tt.expectedError, response["error"])
				}

				if tt.expectedCode != "" && response["code"] != tt.expectedCode {
					t.Errorf("Expected error code '%s', got '%s'", tt.expectedCode, response["code"])
				}
			}

			// For successful requests, verify response structure
			if rr.Code == http.StatusOK {
				var response map[string]interface{}
				err := json.Unmarshal(rr.Body.Bytes(), &response)
				if err != nil {
					t.Errorf("Failed to unmarshal success response: %v", err)
				}

				if response["key"] != tt.key {
					t.Errorf("Expected key '%s' in response, got '%s'", tt.key, response["key"])
				}

				if response["value"] == "" {
					t.Error("Expected value in response")
				}
			}
		})
	}
}

// TestHandleDeleteValidation tests DELETE request validation
func TestHandleDeleteValidation(t *testing.T) {
	server := setupTestServer()
	store := server.store.(*MockKVStore)
	
	// Set up test data
	store.Put("existing-key", "existing-value")

	tests := []struct {
		name           string
		key            string
		expectedStatus int
		expectedError  string
		expectedCode   string
	}{
		{
			name:           "Valid key exists",
			key:            "existing-key",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Valid key not found",
			key:            "nonexistent-key",
			expectedStatus: http.StatusNotFound,
			expectedError:  "Key not found",
			expectedCode:   "KEY_NOT_FOUND",
		},
		{
			name:           "Empty key",
			key:            "",
			expectedStatus: http.StatusNotFound, // 404 because route doesn't match
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("DELETE", "/kv/"+tt.key, nil)
			req = mux.SetURLVars(req, map[string]string{"key": tt.key})

			rr := executeRequest(server, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, rr.Code)
			}

			if tt.expectedError != "" {
				var response map[string]interface{}
				err := json.Unmarshal(rr.Body.Bytes(), &response)
				if err != nil {
					t.Errorf("Failed to unmarshal error response: %v", err)
				}

				if response["error"] != tt.expectedError {
					t.Errorf("Expected error '%s', got '%s'", tt.expectedError, response["error"])
				}

				if tt.expectedCode != "" && response["code"] != tt.expectedCode {
					t.Errorf("Expected error code '%s', got '%s'", tt.expectedCode, response["code"])
				}
			}

			// For successful requests, verify response structure
			if rr.Code == http.StatusOK {
				var response map[string]interface{}
				err := json.Unmarshal(rr.Body.Bytes(), &response)
				if err != nil {
					t.Errorf("Failed to unmarshal success response: %v", err)
				}

				if response["key"] != tt.key {
					t.Errorf("Expected key '%s' in response, got '%s'", tt.key, response["key"])
				}

				if response["message"] == "" {
					t.Error("Expected success message in response")
				}
			}
		})
	}
}

// TestJSONErrorResponseFormat tests that error responses have consistent JSON format
func TestJSONErrorResponseFormat(t *testing.T) {
	server := setupTestServer()

	// Test malformed JSON
	req := httptest.NewRequest("PUT", "/kv/test", bytes.NewBufferString(`{"value": invalid`))
	req.Header.Set("Content-Type", "application/json")
	req = mux.SetURLVars(req, map[string]string{"key": "test"})

	rr := executeRequest(server, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}

	// Verify Content-Type header
	contentType := rr.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type 'application/json', got '%s'", contentType)
	}

	// Verify JSON structure
	var response map[string]interface{}
	err := json.Unmarshal(rr.Body.Bytes(), &response)
	if err != nil {
		t.Errorf("Failed to unmarshal error response: %v", err)
	}

	// Check required fields
	if response["error"] == "" {
		t.Error("Expected 'error' field in response")
	}

	if response["code"] == "" {
		t.Error("Expected 'code' field in response")
	}

	// Check details field exists for INVALID_JSON errors
	if response["code"] == "INVALID_JSON" {
		details, ok := response["details"].(map[string]interface{})
		if !ok {
			t.Error("Expected 'details' field for INVALID_JSON errors")
		} else {
			if details["message"] == "" {
				t.Error("Expected 'message' in details")
			}
			if details["body_length"] == nil {
				t.Error("Expected 'body_length' in details")
			}
			if details["body_preview"] == "" {
				t.Error("Expected 'body_preview' in details")
			}
		}
	}
}