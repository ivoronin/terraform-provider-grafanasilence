# terraform-provider-grafanasilence

Terraform provider for managing Grafana Alertmanager silences

[![Test](https://github.com/ivoronin/terraform-provider-grafanasilence/actions/workflows/test.yml/badge.svg)](https://github.com/ivoronin/terraform-provider-grafanasilence/actions/workflows/test.yml)
[![Release](https://img.shields.io/github/v/release/ivoronin/terraform-provider-grafanasilence)](https://github.com/ivoronin/terraform-provider-grafanasilence/releases)

[Overview](#overview) · [Features](#features) · [Installation](#installation) · [Usage](#usage) · [Configuration](#configuration) · [Requirements](#requirements) · [License](#license)

## Overview

This provider manages Grafana Alertmanager silences through Terraform. Silences suppress alert notifications for a defined time window based on label matchers. The provider communicates with the Grafana Alertmanager API using Bearer token or Basic authentication.

## Features

- Create, update, and delete Alertmanager silences
- Import existing silences by UUID
- Regex and inequality matchers
- `duration` attribute as an alternative to `ends_at` (e.g. `"6h"`, `"30m"`)
- Optional `starts_at` - defaults to current time when omitted
- Automatic handling of naturally expired silences (no spurious recreation)

## Installation

Add the provider to your Terraform configuration:

```hcl
terraform {
  required_providers {
    grafanasilence = {
      source = "ivoronin/grafanasilence"
    }
  }
}
```

Then run `terraform init`.

## Usage

```hcl
provider "grafanasilence" {
  url  = "https://grafana.example.com" # or set GRAFANA_URL
  auth = "glsa_xxxxxxxxxxxx"           # or set GRAFANA_AUTH
}

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
```

Using `duration` instead of `ends_at` (`starts_at` defaults to now):

```hcl
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
```

Import an existing silence:

```bash
terraform import grafanasilence_silence.maintenance <silence-uuid>
```

## Configuration

| Attribute | Description | Required | Environment Variable |
|-----------|-------------|----------|---------------------|
| `url` | Grafana instance base URL | Yes | `GRAFANA_URL` |
| `auth` | API token or `user:pass` for basic auth | Yes | `GRAFANA_AUTH` |

Values containing a colon are treated as Basic auth credentials; all other values are treated as Bearer tokens.

## Requirements

- [Terraform](https://www.terraform.io/downloads) >= 1.0
- [Go](https://golang.org/doc/install) >= 1.23 (building from source only)

## License

[MPL-2.0](LICENSE)
