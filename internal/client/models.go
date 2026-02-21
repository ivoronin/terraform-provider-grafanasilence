// Package client provides an HTTP client for the Grafana Alertmanager silence API.
package client

// Models aligned with the Grafana Alertmanager OpenAPI spec:
// https://github.com/grafana/prometheus-alertmanager/blob/main/api/v2/openapi.yaml

// Matcher represents a silence matcher that determines which alerts are silenced.
type Matcher struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	IsRegex bool   `json:"isRegex"`
	IsEqual *bool  `json:"isEqual,omitempty"`
}

// SilenceState represents the state of a silence as defined by the spec enum.
type SilenceState string

// Possible silence states.
const (
	SilenceStateExpired SilenceState = "expired"
	SilenceStateActive  SilenceState = "active"
	SilenceStatePending SilenceState = "pending"
)

// SilenceStatus represents the status object of a silence.
type SilenceStatus struct {
	State SilenceState `json:"state"`
}

// Silence is the base type matching the spec's "silence" definition.
type Silence struct {
	Matchers  []Matcher `json:"matchers"`
	StartsAt  string    `json:"startsAt"`
	EndsAt    string    `json:"endsAt"`
	CreatedBy string    `json:"createdBy"`
	Comment   string    `json:"comment"`
}

// PostableSilence matches the spec's "postableSilence" (allOf: {id} + silence).
// ID is omitted when creating a new silence and set when updating an existing one.
type PostableSilence struct {
	Silence

	ID string `json:"id,omitempty"`
}

// GettableSilence matches the spec's "gettableSilence" (allOf: {id, status, updatedAt} + silence).
type GettableSilence struct {
	Silence

	ID        string        `json:"id"`
	Status    SilenceStatus `json:"status"`
	UpdatedAt string        `json:"updatedAt"`
}

// PostSilencesOKBody is the response body from the postSilences operation.
type PostSilencesOKBody struct {
	SilenceID string `json:"silenceID"`
}
