resource "grafanasilence_silence" "maintenance" {
  starts_at  = "2026-03-01T00:00:00Z"
  ends_at    = "2026-03-01T06:00:00Z"
  created_by = "terraform"
  comment    = "Scheduled maintenance window"

  matchers {
    name     = "alertname"
    value    = "HighMemoryUsage"
    is_regex = false
  }

  matchers {
    name     = "env"
    value    = "staging"
    is_regex = false
  }
}

# Using duration instead of ends_at (starts_at defaults to now)
resource "grafanasilence_silence" "deployment" {
  duration   = "6h"
  created_by = "terraform"
  comment    = "Deployment silence window"

  matchers {
    name     = "alertname"
    value    = "HighErrorRate"
    is_regex = false
  }
}
