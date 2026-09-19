locals {
  dashboard_path = "${path.module}/../infra/grafana/dashboards"
}

variable "grafana_cloud_stack_slug" {
  type = string
}

data "grafana_cloud_stack" "current" {
  provider = grafana.cloud
  slug     = var.grafana_cloud_stack_slug
}

# DASHBOARDS

resource "grafana_apps_dashboard_dashboard_v1beta1" "oi_overview" {
  provider = grafana.stack

  metadata {
    uid = "openitinerary-overview"
  }

  spec {
    title = "OpenItinerary Overview"
    json  = file("${local.dashboard_path}/openitinerary-overview.json")
  }
}

resource "grafana_apps_dashboard_dashboard_v1beta1" "oibp" {
  provider = grafana.stack
  metadata {
    uid = "oibp-batch-processing"
  }

  spec {
    title = "OpenItinerary Batch Processing"
    json  = file("${local.dashboard_path}/openitinerary-batch-processing.json")
  }
}

resource "grafana_apps_dashboard_dashboard_v1beta1" "oi_adapter" {
  provider = grafana.stack
  metadata {
    uid = "oi-adapter-api"
  }

  spec {
    title = "OpenItinerary Adapter API"
    json  = file("${local.dashboard_path}/openitinerary-adapter-api.json")
  }
}
