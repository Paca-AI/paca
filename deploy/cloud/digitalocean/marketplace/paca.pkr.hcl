# Packer template for the DigitalOcean Marketplace 1-Click "Paca" Droplet image.
# See README.md in this directory for the full build + submission steps.
packer {
  required_plugins {
    digitalocean = {
      source  = "github.com/digitalocean/digitalocean"
      version = ">= 1.1.0"
    }
  }
}

variable "do_token" {
  type      = string
  default   = env("DIGITALOCEAN_TOKEN")
  sensitive = true
}

variable "image_version" {
  type        = string
  description = "Paca release the image is labelled with (e.g. 1.4.0). The Droplet installs the latest release at first boot."
}

variable "region" {
  type    = string
  default = "nyc3"
}

# Clone of https://github.com/digitalocean/marketplace-partners — provides the
# cleanup and validation scripts Marketplace review expects.
variable "marketplace_partners_dir" {
  type = string
}

source "digitalocean" "paca" {
  api_token     = var.do_token
  image         = "ubuntu-24-04-x64"
  region        = var.region
  size          = "s-2vcpu-4gb"   # build on a small size so every Droplet size can use the image
  ssh_username  = "root"
  snapshot_name = "paca-${var.image_version}-{{timestamp}}"
}

build {
  sources = ["source.digitalocean.paca"]

  provisioner "shell" {
    inline = ["cloud-init status --wait"]
  }

  provisioner "file" {
    source      = "${path.root}/files/etc/"
    destination = "/etc/"
  }

  provisioner "file" {
    source      = "${path.root}/files/var/"
    destination = "/var/"
  }

  # The same bootstrap the AWS/GCP/DigitalOcean cloud-init paths use.
  provisioner "file" {
    source      = "${path.root}/../../bootstrap.sh"
    destination = "/tmp/bootstrap.sh"
  }

  provisioner "shell" {
    scripts = [
      "${path.root}/scripts/010-docker.sh",
      "${path.root}/scripts/020-paca.sh",
    ]
  }

  provisioner "shell" {
    scripts = [
      "${var.marketplace_partners_dir}/scripts/90-cleanup.sh",
      "${var.marketplace_partners_dir}/scripts/99-img-check.sh",
    ]
  }
}
