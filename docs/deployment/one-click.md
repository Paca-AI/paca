# One-click deploys

Paca ships one-click paths for **AWS**, **GCP** and **DigitalOcean**. Each
creates a single VM that installs the full stack (API, web, realtime, Postgres,
Valkey, storage and the Agent Runner) with the same non-interactive
`install.sh`, through [`deploy/cloud/bootstrap.sh`](../../deploy/cloud/bootstrap.sh).

| Provider | Path | Button |
|:--|:--|:--|
| AWS | CloudFormation (EC2 + Elastic IP) | Launch Stack; needs the maintainer setup below |
| GCP | Cloud Shell tutorial + `gcloud` script | Opens Cloud Shell |
| DigitalOcean | Droplet + cloud-init | Opens this guide (DigitalOcean has no link that pre-fills a cloud-init script) |

**Why only VMs.** The Agent Runner starts each agent sandbox as a Docker
container through the host's Docker socket. Platforms without one (App
Platform, Render, Railway and similar) can run only part of Paca, so they are
not offered here.

Nothing here has been run against a live account yet; treat the first deploy
on each provider as a test. The VM paths download `bootstrap.sh` from the
latest release, so they need a release that includes it. Default size is about
4–8 GB of RAM (`e2-standard-2`, `t3.large`, or a 4 GB Droplet); 4 GB is the
minimum.

## AWS (CloudFormation)

Template: [`deploy/cloud/aws/paca.cfn.yaml`](../../deploy/cloud/aws/paca.cfn.yaml)
(also attached to every release as `paca.cfn.yaml`). It creates a security
group (80/443), an encrypted-disk Ubuntu 24.04 instance (default `t3.large`),
an Elastic IP, and an SSM-enabled role (no SSH needed).

1. CloudFormation → *Create stack → With new resources → Upload a template file*.
2. Set parameters (all optional) and create the stack.
3. Open the `Url` output after ~5 minutes.

**Launch-Stack button:** CloudFormation quick-create needs the template on S3.
The release workflow uploads it to `s3://paca-cloudformation/latest/paca.cfn.yaml`
(see "AWS button setup" below; `deploy/cloud/aws/publish-template.sh` does it by hand); until then the README button points at a file that doesn't exist yet.

Note: `AdminPassword` ends up in instance user-data (readable by anyone with
`ec2:DescribeInstanceAttribute`). Leave it empty to auto-generate instead.

## GCP (Compute Engine via Cloud Shell)

The button opens Google Cloud Shell with this repo and the walkthrough in
[`deploy/cloud/gcp/tutorial.md`](../../deploy/cloud/gcp/tutorial.md). The
tutorial runs [`deploy.sh`](../../deploy/cloud/gcp/deploy.sh), which enables
the Compute API, reserves a static IP, opens ports 80/443 and creates an
Ubuntu 24.04 `e2-standard-2` VM whose startup script runs `bootstrap.sh`
(same as the DigitalOcean/AWS VMs). Cloud Shell is already logged in, so no
credentials are needed; you only pick a billing-enabled project. Set
`PACA_DOMAIN`, `ADMIN_PASSWORD`, `PACA_ZONE` or `PACA_MACHINE` to customise.
Like the other VM paths it needs a release that includes `bootstrap.sh`.

## DigitalOcean (Droplet)

1. Create an **Ubuntu 24.04** Droplet, **4 GB RAM / 2 vCPU or more**.
2. Under *Advanced Options → Add Initialization scripts*, paste
   [`deploy/cloud/digitalocean-cloud-init.yaml`](../../deploy/cloud/digitalocean-cloud-init.yaml).
   Fill in `PACA_DOMAIN` (DNS already pointing at the Droplet → HTTPS) and/or
   `ADMIN_PASSWORD`, or leave both empty.
3. After ~5 minutes open `http://<droplet-ip>`. Admin credentials:
   `ssh root@<ip> grep ADMIN_ /opt/paca/.env` (progress: `/var/log/paca-install.log`).

## Maintainer setup for the AWS button

**AWS button setup** (one time; CI uses GitHub OIDC, no stored keys):

1. Create an S3 bucket named `paca-cloudformation` in `us-east-1`. Turn off
   "Block all public access" and add a bucket policy allowing `s3:GetObject`
   on `arn:aws:s3:::paca-cloudformation/*` to `*`.
2. IAM → Identity providers → add `https://token.actions.githubusercontent.com`
   with audience `sts.amazonaws.com`.
3. Create a role trusted by that provider, with condition
   `token.actions.githubusercontent.com:sub` = `repo:Paca-AI/paca:*`, and a
   policy allowing only `s3:PutObject` on `paca-cloudformation/*`.
4. In GitHub → Settings → Secrets and variables → Actions → *Variables*, add
   `AWS_TEMPLATE_BUCKET=paca-cloudformation` and `AWS_ROLE_ARN=<role arn>`.
5. Each non-prerelease release now uploads the template. To publish before the
   next release, run `bash deploy/cloud/aws/publish-template.sh` with your own
   AWS login.
