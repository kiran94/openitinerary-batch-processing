variable "grafana_cloud_stack_slug" {
  type = string
}

data "grafana_cloud_stack" "current" {
  provider = grafana.cloud
  slug     = var.grafana_cloud_stack_slug
}
