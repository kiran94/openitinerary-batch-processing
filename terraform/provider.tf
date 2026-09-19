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

# Cloud Portal / Grafana Cloud API provider.
# Manages resources such as stacks and service accounts
# cloud access policy permissions: stacks:read
provider "grafana" {
  alias                     = "cloud"
  cloud_access_policy_token = var.grafana_cloud_access_policy_token
}

# Stack-level Grafana HTTP API provider.
# Manages resources such as dashboards, alerting etc
# NOTE: the token passed must be related to a svc account as dashboards cannot be created from a
# cloud access policy
provider "grafana" {
  alias    = "stack"
  url      = data.grafana_cloud_stack.current.url
  stack_id = data.grafana_cloud_stack.current.id
  auth     = var.grafana_stack_service_account_token
}

variable "grafana_cloud_access_policy_token" {
  type      = string
  sensitive = true
}

variable "grafana_stack_service_account_token" {
  type      = string
  sensitive = true
}
