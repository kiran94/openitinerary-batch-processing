variable "grafana_prometheus_name" {
  type = string
}

data "grafana_data_source" "from_name" {
  provider = grafana.stack
  name     = var.grafana_prometheus_name
}
