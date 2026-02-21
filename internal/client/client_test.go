package client_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ivoronin/terraform-provider-grafanasilence/internal/client"
)

const testUUID = "test-uuid-1234"

func handlePostSilences(writer http.ResponseWriter, request *http.Request) {
	if request.Header.Get("Authorization") != "Bearer test-token" {
		http.Error(writer, "unauthorized", http.StatusUnauthorized)

		return
	}

	var postable client.PostableSilence

	err := json.NewDecoder(request.Body).Decode(&postable)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)

		return
	}

	writer.Header().Set("Content-Type", "application/json")

	err = json.NewEncoder(writer).Encode(client.PostSilencesOKBody{SilenceID: testUUID})
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
	}
}

func handleGetSilence(writer http.ResponseWriter, request *http.Request) {
	silenceID := request.PathValue("id")
	if silenceID == "not-found" {
		http.Error(writer, "not found", http.StatusNotFound)

		return
	}

	writer.Header().Set("Content-Type", "application/json")

	err := json.NewEncoder(writer).Encode(client.GettableSilence{
		ID:        silenceID,
		Status:    client.SilenceStatus{State: client.SilenceStateActive},
		UpdatedAt: "2026-03-01T00:00:00Z",
		Silence: client.Silence{
			StartsAt:  "2026-03-01T00:00:00Z",
			EndsAt:    "2026-03-01T06:00:00Z",
			CreatedBy: "terraform",
			Comment:   "test silence",
			Matchers: []client.Matcher{
				{Name: "alertname", Value: "TestAlert", IsRegex: false},
			},
		},
	})
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
	}
}

func handleDeleteSilence(writer http.ResponseWriter, request *http.Request) {
	silenceID := request.PathValue("id")
	if silenceID == "not-found" {
		http.Error(writer, "not found", http.StatusNotFound)

		return
	}

	writer.WriteHeader(http.StatusOK)
}

func setupTestClient(t *testing.T) *client.Client {
	t.Helper()

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)

	mux.HandleFunc("POST /api/alertmanager/grafana/api/v2/silences", handlePostSilences)
	mux.HandleFunc("GET /api/alertmanager/grafana/api/v2/silence/{id}", handleGetSilence)
	mux.HandleFunc("DELETE /api/alertmanager/grafana/api/v2/silence/{id}", handleDeleteSilence)

	apiClient := client.New(server.URL, "test-token")
	t.Cleanup(server.Close)

	return apiClient
}

func TestCreateSilence(t *testing.T) {
	apiClient := setupTestClient(t)

	silenceID, err := apiClient.PostSilences(context.Background(), client.PostableSilence{
		Silence: client.Silence{
			StartsAt:  "2026-03-01T00:00:00Z",
			EndsAt:    "2026-03-01T06:00:00Z",
			CreatedBy: "terraform",
			Comment:   "test silence",
			Matchers: []client.Matcher{
				{Name: "alertname", Value: "TestAlert", IsRegex: false},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if silenceID != testUUID {
		t.Fatalf("expected id %s, got %s", testUUID, silenceID)
	}
}

func TestGetSilence(t *testing.T) {
	apiClient := setupTestClient(t)

	silence, err := apiClient.GetSilence(context.Background(), testUUID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if silence == nil {
		t.Fatal("expected silence, got nil")
	}

	if silence.ID != testUUID {
		t.Fatalf("expected id %s, got %s", testUUID, silence.ID)
	}

	if silence.Status.State != client.SilenceStateActive {
		t.Fatalf("expected state active, got %s", silence.Status.State)
	}
}

func TestGetSilenceNotFound(t *testing.T) {
	apiClient := setupTestClient(t)

	_, err := apiClient.GetSilence(context.Background(), "not-found")
	if !errors.Is(err, client.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestUpdateSilence(t *testing.T) {
	apiClient := setupTestClient(t)

	silenceID, err := apiClient.PostSilences(context.Background(), client.PostableSilence{
		ID: testUUID,
		Silence: client.Silence{
			StartsAt:  "2026-03-01T00:00:00Z",
			EndsAt:    "2026-03-01T06:00:00Z",
			CreatedBy: "terraform",
			Comment:   "updated silence",
			Matchers: []client.Matcher{
				{Name: "alertname", Value: "TestAlert", IsRegex: false},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if silenceID != testUUID {
		t.Fatalf("expected id %s, got %s", testUUID, silenceID)
	}
}

func TestDeleteSilence(t *testing.T) {
	apiClient := setupTestClient(t)

	err := apiClient.DeleteSilence(context.Background(), testUUID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteSilenceNotFound(t *testing.T) {
	apiClient := setupTestClient(t)

	err := apiClient.DeleteSilence(context.Background(), "not-found")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBasicAuth(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	var receivedAuth string

	mux.HandleFunc(
		"GET /api/alertmanager/grafana/api/v2/silence/{id}",
		func(writer http.ResponseWriter, request *http.Request) {
			receivedAuth = request.Header.Get("Authorization")
			writer.Header().Set("Content-Type", "application/json")

			err := json.NewEncoder(writer).Encode(client.GettableSilence{
				ID:     "test",
				Status: client.SilenceStatus{State: client.SilenceStateActive},
				Silence: client.Silence{
					Matchers: []client.Matcher{},
				},
			})
			if err != nil {
				http.Error(writer, err.Error(), http.StatusInternalServerError)
			}
		},
	)

	apiClient := client.New(server.URL, "admin:secret")

	_, err := apiClient.GetSilence(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// "admin:secret" base64 encoded is "YWRtaW46c2VjcmV0"
	expected := "Basic YWRtaW46c2VjcmV0"
	if receivedAuth != expected {
		t.Fatalf("expected auth header %q, got %q", expected, receivedAuth)
	}
}
