package provider_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/ivoronin/terraform-provider-grafanasilence/internal/client"
)

var errUnexpectedDeleteCount = errors.New("unexpected delete call count")

type testServer struct {
	*httptest.Server

	mu          sync.Mutex
	silences    map[string]*client.GettableSilence
	nextID      int
	autoExpire  bool
	deleteCalls []string
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()

	server := &testServer{
		silences:   map[string]*client.GettableSilence{},
		nextID:     1,
		autoExpire: true,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/alertmanager/grafana/api/v2/silences", server.handleCreateSilence)
	mux.HandleFunc("GET /api/alertmanager/grafana/api/v2/silence/{id}", server.handleGetSilence)
	mux.HandleFunc("DELETE /api/alertmanager/grafana/api/v2/silence/{id}", server.handleDeleteSilence)

	server.Server = httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return server
}

// ExpireSilence manually sets a silence's status to expired (simulates manual expiry via API).
func (server *testServer) ExpireSilence(silenceID string) {
	server.mu.Lock()
	defer server.mu.Unlock()

	if silence, ok := server.silences[silenceID]; ok {
		silence.Status.State = client.SilenceStateExpired
	}
}

// RemoveSilence deletes a silence from the server (simulates post-retention 404).
func (server *testServer) RemoveSilence(silenceID string) {
	server.mu.Lock()
	defer server.mu.Unlock()

	delete(server.silences, silenceID)
}

// DeleteCallCount returns the number of DELETE API calls made to the server.
func (server *testServer) DeleteCallCount() int {
	server.mu.Lock()
	defer server.mu.Unlock()

	return len(server.deleteCalls)
}

// AddSilence seeds a silence directly into the server state.
func (server *testServer) AddSilence(silence *client.GettableSilence) {
	server.mu.Lock()
	defer server.mu.Unlock()

	server.silences[silence.ID] = silence
}

func (server *testServer) handleCreateSilence(writer http.ResponseWriter, request *http.Request) {
	server.mu.Lock()
	defer server.mu.Unlock()

	var postable client.PostableSilence

	err := json.NewDecoder(request.Body).Decode(&postable)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)

		return
	}

	silenceID := postable.ID

	// Match real Grafana behavior: expired silences cannot be updated in place.
	// A new silence is created with a fresh ID instead.
	existing, isUpdate := server.silences[silenceID]
	if silenceID == "" || (isUpdate && existing.Status.State == client.SilenceStateExpired) {
		silenceID = fmt.Sprintf("test-silence-%d", server.nextID)
		server.nextID++
	}

	// Simulate real Grafana: re-format timestamps with millisecond precision.
	startsAt := addMillis(postable.StartsAt)
	endsAt := addMillis(postable.EndsAt)

	server.silences[silenceID] = &client.GettableSilence{
		ID:        silenceID,
		Status:    client.SilenceStatus{State: client.SilenceStateActive},
		UpdatedAt: "2026-03-01T00:00:00.000Z",
		Silence: client.Silence{
			StartsAt:  startsAt,
			EndsAt:    endsAt,
			CreatedBy: postable.CreatedBy,
			Comment:   postable.Comment,
			Matchers:  postable.Matchers,
		},
	}

	writer.Header().Set("Content-Type", "application/json")

	err = json.NewEncoder(writer).Encode(client.PostSilencesOKBody{SilenceID: silenceID})
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
	}
}

func (server *testServer) handleGetSilence(writer http.ResponseWriter, request *http.Request) {
	server.mu.Lock()
	defer server.mu.Unlock()

	silenceID := request.PathValue("id")

	silence, ok := server.silences[silenceID]
	if !ok {
		http.Error(writer, "not found", http.StatusNotFound)

		return
	}

	// Auto-expire: if endsAt is in the past, return as expired (simulates real Grafana)
	if server.autoExpire {
		endsAt, err := time.Parse(time.RFC3339, silence.EndsAt)
		if err == nil && time.Now().After(endsAt) {
			silence.Status.State = client.SilenceStateExpired
		}
	}

	writer.Header().Set("Content-Type", "application/json")

	err := json.NewEncoder(writer).Encode(silence)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
	}
}

func (server *testServer) handleDeleteSilence(writer http.ResponseWriter, request *http.Request) {
	server.mu.Lock()
	defer server.mu.Unlock()

	silenceID := request.PathValue("id")
	server.deleteCalls = append(server.deleteCalls, silenceID)

	if silence, ok := server.silences[silenceID]; ok {
		silence.Status.State = client.SilenceStateExpired
	}

	writer.WriteHeader(http.StatusOK)
}

func setupTestEnv(t *testing.T, server *testServer) {
	t.Helper()

	t.Setenv("GRAFANA_URL", server.URL)
	t.Setenv("GRAFANA_AUTH", "test-token")
}

// addMillis re-formats an RFC3339 timestamp with millisecond precision,
// simulating real Grafana API responses.
func addMillis(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}

	return t.Format("2006-01-02T15:04:05.000Z07:00")
}

const testAccSilenceCreateConfig = `
resource "grafanasilence_silence" "test" {
  starts_at  = "2026-03-01T00:00:00Z"
  ends_at    = "2026-03-01T06:00:00Z"
  created_by = "terraform"
  comment    = "Test silence"

  matchers {
    name     = "alertname"
    value    = "TestAlert"
    is_regex = false
  }
}
`

const testAccSilenceUpdateConfig = `
resource "grafanasilence_silence" "test" {
  starts_at  = "2026-03-01T00:00:00Z"
  ends_at    = "2026-03-01T12:00:00Z"
  created_by = "terraform"
  comment    = "Updated test silence"

  matchers {
    name     = "alertname"
    value    = "TestAlert"
    is_regex = false
  }

  matchers {
    name     = "env"
    value    = "staging"
    is_regex = false
  }
}
`

func TestAccSilenceResource(t *testing.T) {
	server := newTestServer(t)
	setupTestEnv(t, server)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and read
			{
				Config: testAccSilenceCreateConfig,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("grafanasilence_silence.test", "id", "test-silence-1"),
					resource.TestCheckResourceAttr("grafanasilence_silence.test", "starts_at", "2026-03-01T00:00:00Z"),
					resource.TestCheckResourceAttr("grafanasilence_silence.test", "ends_at", "2026-03-01T06:00:00Z"),
					resource.TestCheckResourceAttr("grafanasilence_silence.test", "created_by", "terraform"),
					resource.TestCheckResourceAttr("grafanasilence_silence.test", "comment", "Test silence"),
					resource.TestCheckResourceAttr("grafanasilence_silence.test", "status", "active"),
					resource.TestCheckResourceAttr("grafanasilence_silence.test", "matchers.#", "1"),
					resource.TestCheckResourceAttr("grafanasilence_silence.test", "matchers.0.name", "alertname"),
					resource.TestCheckResourceAttr("grafanasilence_silence.test", "matchers.0.value", "TestAlert"),
					resource.TestCheckResourceAttr("grafanasilence_silence.test", "matchers.0.is_regex", "false"),
					resource.TestCheckResourceAttr("grafanasilence_silence.test", "matchers.0.is_equal", "true"),
				),
			},
			// Update
			{
				Config: testAccSilenceUpdateConfig,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("grafanasilence_silence.test", "ends_at", "2026-03-01T12:00:00Z"),
					resource.TestCheckResourceAttr("grafanasilence_silence.test", "comment", "Updated test silence"),
					resource.TestCheckResourceAttr("grafanasilence_silence.test", "matchers.#", "2"),
				),
			},
			// Import
			{
				ResourceName:            "grafanasilence_silence.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"starts_at", "ends_at"},
			},
		},
	})
}

func TestAccSilenceResourceWithIsEqual(t *testing.T) {
	server := newTestServer(t)
	setupTestEnv(t, server)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "grafanasilence_silence" "negative" {
  starts_at  = "2026-03-01T00:00:00Z"
  ends_at    = "2026-03-01T06:00:00Z"
  created_by = "terraform"
  comment    = "Negative matcher test"

  matchers {
    name     = "env"
    value    = "production"
    is_regex = false
    is_equal = false
  }
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("grafanasilence_silence.negative", "matchers.0.is_equal", "false"),
				),
			},
		},
	})
}

// Case 1: Natural expiry, within retention.
// Silence has past endsAt, Grafana returns status=expired.
// Read keeps resource in state (no recreation).
func TestAccSilenceNaturalExpiry(t *testing.T) {
	server := newTestServer(t)
	server.autoExpire = false // Control expiry manually via PreConfig
	setupTestEnv(t, server)

	pastEndsAt := time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339)
	config := fmt.Sprintf(`
resource "grafanasilence_silence" "test" {
  starts_at  = "2020-01-01T00:00:00Z"
  ends_at    = "%s"
  created_by = "terraform"
  comment    = "Natural expiry test"

  matchers {
    name     = "alertname"
    value    = "TestAlert"
    is_regex = false
  }
}
`, pastEndsAt)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("grafanasilence_silence.test", "id", "test-silence-1"),
					resource.TestCheckResourceAttr("grafanasilence_silence.test", "status", "active"),
				),
			},
			{
				PreConfig: func() {
					server.ExpireSilence("test-silence-1")
				},
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					// Desired: ID unchanged (no recreation), status is expired
					resource.TestCheckResourceAttr("grafanasilence_silence.test", "id", "test-silence-1"),
					resource.TestCheckResourceAttr("grafanasilence_silence.test", "status", "expired"),
				),
			},
		},
	})
}

// Case 2: Natural expiry, past retention (404).
// Silence has past endsAt, Grafana returns 404 (removed after retention).
// Desired: Read keeps resource in state (no recreation).
func TestAccSilenceNaturalExpiryNotFound(t *testing.T) {
	server := newTestServer(t)
	server.autoExpire = false // Control expiry manually via PreConfig
	setupTestEnv(t, server)

	pastEndsAt := time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339)
	config := fmt.Sprintf(`
resource "grafanasilence_silence" "test" {
  starts_at  = "2020-01-01T00:00:00Z"
  ends_at    = "%s"
  created_by = "terraform"
  comment    = "Natural expiry gone test"

  matchers {
    name     = "alertname"
    value    = "TestAlert"
    is_regex = false
  }
}
`, pastEndsAt)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("grafanasilence_silence.test", "id", "test-silence-1"),
					resource.TestCheckResourceAttr("grafanasilence_silence.test", "status", "active"),
				),
			},
			{
				PreConfig: func() {
					server.RemoveSilence("test-silence-1")
				},
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					// Desired: ID unchanged (no recreation), status is expired
					resource.TestCheckResourceAttr("grafanasilence_silence.test", "id", "test-silence-1"),
					resource.TestCheckResourceAttr("grafanasilence_silence.test", "status", "expired"),
				),
			},
		},
	})
}

// Case 3: Manual expiry, within retention - should pass.
// Silence has future endsAt but was manually expired. Recreation is correct.
func TestAccSilenceManualExpiry(t *testing.T) {
	server := newTestServer(t)
	setupTestEnv(t, server)

	config := `
resource "grafanasilence_silence" "test" {
  starts_at  = "2026-03-01T00:00:00Z"
  ends_at    = "2026-03-01T06:00:00Z"
  created_by = "terraform"
  comment    = "Manual expiry test"

  matchers {
    name     = "alertname"
    value    = "TestAlert"
    is_regex = false
  }
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("grafanasilence_silence.test", "id", "test-silence-1"),
				),
			},
			{
				PreConfig: func() {
					server.ExpireSilence("test-silence-1")
				},
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					// Recreation is correct: new ID assigned
					resource.TestCheckResourceAttr("grafanasilence_silence.test", "id", "test-silence-2"),
				),
			},
		},
	})
}

// Case 4: Manual expiry, past retention (404) - should pass.
// Silence has future endsAt and was removed from Grafana. Recreation is correct.
func TestAccSilenceManualExpiryGone(t *testing.T) {
	server := newTestServer(t)
	setupTestEnv(t, server)

	config := `
resource "grafanasilence_silence" "test" {
  starts_at  = "2026-03-01T00:00:00Z"
  ends_at    = "2026-03-01T06:00:00Z"
  created_by = "terraform"
  comment    = "Manual expiry gone test"

  matchers {
    name     = "alertname"
    value    = "TestAlert"
    is_regex = false
  }
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("grafanasilence_silence.test", "id", "test-silence-1"),
				),
			},
			{
				PreConfig: func() {
					server.RemoveSilence("test-silence-1")
				},
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					// Recreation is correct: new ID assigned
					resource.TestCheckResourceAttr("grafanasilence_silence.test", "id", "test-silence-2"),
				),
			},
		},
	})
}

// Case 5: Create with past endsAt.
// Grafana auto-expires the silence immediately.
// Read keeps expired silence with past endsAt in state; refresh plan is empty.
func TestAccSilenceCreateWithPastEndsAt(t *testing.T) {
	server := newTestServer(t)
	setupTestEnv(t, server)

	pastEndsAt := time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339)
	config := fmt.Sprintf(`
resource "grafanasilence_silence" "test" {
  starts_at  = "2020-01-01T00:00:00Z"
  ends_at    = "%s"
  created_by = "terraform"
  comment    = "Past endsAt test"

  matchers {
    name     = "alertname"
    value    = "TestAlert"
    is_regex = false
  }
}
`, pastEndsAt)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("grafanasilence_silence.test", "id", "test-silence-1"),
				),
			},
		},
	})
}

// Case 6: Update endsAt from future to past.
// After the update, Grafana auto-expires the silence.
// Read keeps expired silence with past endsAt in state; refresh plan is empty.
func TestAccSilenceUpdateEndsAtToPast(t *testing.T) {
	server := newTestServer(t)
	setupTestEnv(t, server)

	pastEndsAt := time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "grafanasilence_silence" "test" {
  starts_at  = "2026-03-01T00:00:00Z"
  ends_at    = "2026-03-01T06:00:00Z"
  created_by = "terraform"
  comment    = "Update to past test"

  matchers {
    name     = "alertname"
    value    = "TestAlert"
    is_regex = false
  }
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("grafanasilence_silence.test", "status", "active"),
				),
			},
			{
				Config: fmt.Sprintf(`
resource "grafanasilence_silence" "test" {
  starts_at  = "2020-01-01T00:00:00Z"
  ends_at    = "%s"
  created_by = "terraform"
  comment    = "Update to past test"

  matchers {
    name     = "alertname"
    value    = "TestAlert"
    is_regex = false
  }
}
`, pastEndsAt),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("grafanasilence_silence.test", "status", "expired"),
				),
			},
		},
	})
}

// Case 6b: Update expired silence endsAt from past to future.
// The silence was created with a past endsAt (auto-expired by Grafana).
// Then the config changes endsAt to the future.
// Grafana cannot update expired silences - it creates a new one with a new ID.
// The provider should handle this: Update sends the old ID, PostSilences returns
// a new ID, and the provider stores the new ID in state.
func TestAccSilenceUpdateExpiredEndsAtToFuture(t *testing.T) {
	server := newTestServer(t)
	setupTestEnv(t, server)

	pastEndsAt := time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create with past endsAt - Grafana auto-expires it immediately
			{
				Config: fmt.Sprintf(`
resource "grafanasilence_silence" "test" {
  starts_at  = "2020-01-01T00:00:00Z"
  ends_at    = "%s"
  created_by = "terraform"
  comment    = "Update expired to future test"

  matchers {
    name     = "alertname"
    value    = "TestAlert"
    is_regex = false
  }
}
`, pastEndsAt),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("grafanasilence_silence.test", "id", "test-silence-1"),
					resource.TestCheckResourceAttr("grafanasilence_silence.test", "status", "expired"),
				),
			},
			// Update endsAt to future - Grafana creates a new silence with a new ID
			{
				Config: `
resource "grafanasilence_silence" "test" {
  starts_at  = "2026-03-01T00:00:00Z"
  ends_at    = "2026-03-01T06:00:00Z"
  created_by = "terraform"
  comment    = "Update expired to future test"

  matchers {
    name     = "alertname"
    value    = "TestAlert"
    is_regex = false
  }
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					// New ID assigned because Grafana created a new silence
					resource.TestCheckResourceAttr("grafanasilence_silence.test", "id", "test-silence-2"),
					resource.TestCheckResourceAttr("grafanasilence_silence.test", "status", "active"),
				),
			},
		},
	})
}

// Case 7: Import expired silence - should pass.
func TestAccSilenceImportExpired(t *testing.T) {
	server := newTestServer(t)
	setupTestEnv(t, server)

	isEqual := true
	server.AddSilence(&client.GettableSilence{
		ID:        "expired-silence",
		Status:    client.SilenceStatus{State: client.SilenceStateExpired},
		UpdatedAt: "2026-01-01T00:00:00Z",
		Silence: client.Silence{
			StartsAt:  "2026-01-01T00:00:00Z",
			EndsAt:    "2026-01-01T06:00:00Z",
			CreatedBy: "terraform",
			Comment:   "Expired silence",
			Matchers:  []client.Matcher{{Name: "alertname", Value: "Test", IsRegex: false, IsEqual: &isEqual}},
		},
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				ResourceName:  "grafanasilence_silence.test",
				ImportState:   true,
				ImportStateId: "expired-silence",
				ExpectError:   regexp.MustCompile(`expired`),
				Config: `
resource "grafanasilence_silence" "test" {
  starts_at  = "2026-01-01T00:00:00Z"
  ends_at    = "2026-01-01T06:00:00Z"
  created_by = "terraform"
  comment    = "Expired silence"

  matchers {
    name     = "alertname"
    value    = "Test"
    is_regex = false
  }
}
`,
			},
		},
	})
}

// Case 8: Import non-existent silence - should pass.
func TestAccSilenceImportNotFound(t *testing.T) {
	server := newTestServer(t)
	setupTestEnv(t, server)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				ResourceName:  "grafanasilence_silence.test",
				ImportState:   true,
				ImportStateId: "nonexistent-id",
				ExpectError:   regexp.MustCompile(`not found`),
				Config: `
resource "grafanasilence_silence" "test" {
  starts_at  = "2026-01-01T00:00:00Z"
  ends_at    = "2026-01-01T06:00:00Z"
  created_by = "terraform"
  comment    = "Placeholder"

  matchers {
    name     = "alertname"
    value    = "Test"
    is_regex = false
  }
}
`,
			},
		},
	})
}

// Case 9: Delete naturally expired silence - should not call the API.
func TestAccSilenceDeleteNaturallyExpired(t *testing.T) {
	server := newTestServer(t)
	server.autoExpire = false
	setupTestEnv(t, server)

	pastEndsAt := time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339)
	config := fmt.Sprintf(`
resource "grafanasilence_silence" "test" {
  starts_at  = "2020-01-01T00:00:00Z"
  ends_at    = "%s"
  created_by = "terraform"
  comment    = "Delete naturally expired test"

  matchers {
    name     = "alertname"
    value    = "TestAlert"
    is_regex = false
  }
}
`, pastEndsAt)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: func(_ *terraform.State) error {
			if server.DeleteCallCount() != 0 {
				return fmt.Errorf(
					"%w: expected 0, got %d",
					errUnexpectedDeleteCount,
					server.DeleteCallCount(),
				)
			}

			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("grafanasilence_silence.test", "id", "test-silence-1"),
					resource.TestCheckResourceAttr("grafanasilence_silence.test", "status", "active"),
				),
			},
			{
				PreConfig: func() {
					server.ExpireSilence("test-silence-1")
				},
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("grafanasilence_silence.test", "id", "test-silence-1"),
					resource.TestCheckResourceAttr("grafanasilence_silence.test", "status", "expired"),
				),
			},
		},
	})
}

// Case 10: Delete active silence - should call the API.
func TestAccSilenceDeleteActive(t *testing.T) {
	server := newTestServer(t)
	setupTestEnv(t, server)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: func(_ *terraform.State) error {
			if server.DeleteCallCount() != 1 {
				return fmt.Errorf(
					"%w: expected 1, got %d",
					errUnexpectedDeleteCount,
					server.DeleteCallCount(),
				)
			}

			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: `
resource "grafanasilence_silence" "test" {
  starts_at  = "2026-03-01T00:00:00Z"
  ends_at    = "2026-03-01T06:00:00Z"
  created_by = "terraform"
  comment    = "Delete active test"

  matchers {
    name     = "alertname"
    value    = "TestAlert"
    is_regex = false
  }
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("grafanasilence_silence.test", "id", "test-silence-1"),
					resource.TestCheckResourceAttr("grafanasilence_silence.test", "status", "active"),
				),
			},
		},
	})
}
