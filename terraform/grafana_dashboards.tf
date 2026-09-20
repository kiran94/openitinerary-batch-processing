locals {
  dashboard_path = "${path.module}/../infra/grafana/dashboards"
}

resource "grafana_folder" "oibp_dashboard_folder" {
  provider = grafana.stack
  title    = "OIBP Dashboards"
}

resource "grafana_apps_dashboard_dashboard_v1beta1" "oi_overview" {
  provider = grafana.stack

  metadata {
    uid        = "openitinerary-overview"
    folder_uid = grafana_folder.oibp_dashboard_folder.uid
  }

  spec {
    title = "OpenItinerary Overview"
    json  = file("${local.dashboard_path}/openitinerary-overview.json")
  }
}

resource "grafana_apps_dashboard_dashboard_v1beta1" "oibp" {
  provider = grafana.stack
  metadata {
    uid        = "oibp-batch-processing"
    folder_uid = grafana_folder.oibp_dashboard_folder.uid
  }

  spec {
    title = "OpenItinerary Batch Processing"
    json  = file("${local.dashboard_path}/openitinerary-batch-processing.json")
  }
}

resource "grafana_apps_dashboard_dashboard_v1beta1" "oi_adapter" {
  provider = grafana.stack
  metadata {
    uid        = "oi-adapter-api"
    folder_uid = grafana_folder.oibp_dashboard_folder.uid
  }

  spec {
    title = "OpenItinerary Adapter API"
    json  = file("${local.dashboard_path}/openitinerary-adapter-api.json")
  }
}
