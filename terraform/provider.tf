terraform {

  backend "s3" {
    bucket       = "terraform-kiran"
    key          = "openitinerary-batch-processing.tfstate"
    use_lockfile = true
  }

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
    grafana = {
      source  = "grafana/grafana"
      version = "~> 4.46.0"
    }
  }
}

## AWS

variable "aws_region" {
  type    = string
  default = "eu-west-2"
}

provider "aws" {
  region = var.aws_region
}

data "aws_caller_identity" "current" {}

# GRAFANA

# Manages Cloud API resources such as access policies and tokens.
provider "grafana" {
  alias                     = "cloud"
  cloud_access_policy_token = var.grafana_cloud_access_policy_token
}

# Manages Grafana instance configuration such as folders, dashboards, and alerting
provider "grafana" {
  alias = "stack"
  url   = data.grafana_cloud_stack.current.url
  auth  = var.grafana_stack_service_account_token
}

variable "grafana_cloud_access_policy_token" {
  type      = string
  sensitive = true
}

variable "grafana_stack_service_account_token" {
  type      = string
  sensitive = true
}
