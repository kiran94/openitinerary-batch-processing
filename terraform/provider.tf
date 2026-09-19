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
  }
}

variable "aws_region" {
  type    = string
  default = "eu-west-2"
}

provider "aws" {
  region = var.aws_region
}

data "aws_caller_identity" "current" {}
