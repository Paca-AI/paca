# DigitalOcean Marketplace 1-Click image

This builds the Droplet image behind a real "Deploy to DigitalOcean" button.
DigitalOcean has no link that pre-fills a cloud-init script, so the only way to
get a true one-click Droplet is a Marketplace 1-Click App. The image holds
Docker, a firewall and the first-boot installer; every new Droplet generates
its own secrets, database and public IP when it first starts.

## Build

You need [Packer](https://developer.hashicorp.com/packer) and a DigitalOcean API
token (read + write).

```bash
git clone https://github.com/digitalocean/marketplace-partners.git /tmp/marketplace-partners
export DIGITALOCEAN_TOKEN=<token>
packer init deploy/cloud/digitalocean/marketplace
packer build \
  -var image_version=<paca release, e.g. 1.4.0> \
  -var marketplace_partners_dir=/tmp/marketplace-partners \
  deploy/cloud/digitalocean/marketplace
```

The build Droplet is created, configured, cleaned, validated and snapshotted;
the snapshot shows up under *Images → Snapshots*. The last step runs
DigitalOcean's own `99-img_check.sh`; fix anything it flags before submitting.
(The template expects `scripts/90-cleanup.sh` and `scripts/99-img_check.sh` in
that repo; adjust the two paths in `paca.pkr.hcl` if they were renamed.)

## Test the snapshot

Create a Droplet from the snapshot (4 GB+), wait ~5 minutes and open
`http://<droplet-ip>`. Read the admin login with
`ssh root@<ip> grep ADMIN_ /opt/paca/.env`.

## Submit to the Marketplace

1. Apply at <https://marketplace.digitalocean.com/vendors> (Vendor Portal).
2. Create a **1-Click App** listing, pick the snapshot, add name, logo,
   description and the support/docs links.
3. Submit for review. DigitalOcean reviews the image and listing and may ask
   for changes; expect days to weeks.

## Make the README button deploy

After approval the listing has an app slug. Change the DigitalOcean button in
the root README from the guide link to

```
https://cloud.digitalocean.com/droplets/new?appId=<APP_ID>&image=<APP_SLUG>
```

(copy the exact "Create Droplet" link from the approved listing page).
Rebuild and resubmit an image when you want to refresh the base OS.
