# One-click deploys

Paca ships one-click paths for **AWS**, **GCP** and **DigitalOcean**. Each
creates a single VM that installs the full stack (API, web, realtime, Postgres,
Valkey, storage and the Agent Runner) with the same non-interactive
`install.sh`, through [`deploy/cloud/bootstrap.sh`](../../deploy/cloud/bootstrap.sh).

| Provider | Path | Button |
|:--|:--|:--|
| AWS | CloudFormation (EC2 + Elastic IP) | Launch Stack; needs the maintainer setup below |
| GCP | Cloud Shell tutorial + `gcloud` script | Opens Cloud Shell |
| AWS (lightweight) | `paca-lite.cfn.yaml`: Fargate + RDS + ElastiCache + S3 | Launch Stack; **no Agent Runner** |
| GCP (lightweight) | Cloud Shell + `deploy-lite.sh`: Cloud Run + Cloud SQL + Memorystore + GCS | Opens Cloud Shell; **no Agent Runner** |
| Render | Blueprint (`render.yaml`) | Deploys directly; **no Agent Runner** |
| Railway | Template built in Railway's dashboard | Opens the recipe until the template is published; **no Agent Runner** |
| DigitalOcean App Platform | `.do/deploy.template.yaml` | Deploys directly; **no Agent Runner** |
| DigitalOcean Droplet | Droplet + cloud-init | Opens this guide (DigitalOcean has no link that pre-fills a cloud-init script) |

**Why VMs are the full option.** The Agent Runner starts each agent sandbox as a Docker
container through the host's Docker socket. Platforms without one (App
Platform, Render, Railway and similar) can run only part of Paca, so they are
only offered as explicit lighter options without agents (DigitalOcean App Platform, Render, Railway).

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

## AWS lightweight (managed services, no Agent Runner)

Template: [`deploy/cloud/aws/paca-lite.cfn.yaml`](../../deploy/cloud/aws/paca-lite.cfn.yaml)
(also a release asset). It builds a small VPC and runs **one Fargate task**
holding four containers (Caddy gateway, API, web, realtime) behind an
Application Load Balancer, with **RDS Postgres**, **ElastiCache Valkey** and an
**S3 bucket** for files. Secrets are generated into Secrets Manager.

1. CloudFormation → *Create stack*, using the Launch Stack link or the uploaded
   template. Acknowledge the IAM capability.
2. Optional parameters: `CertificateArn` + `Domain` for HTTPS (create an ACM
   certificate first, then point the domain's CNAME/ALIAS at the
   `LoadBalancerDns` output). With neither, Paca is served over plain HTTP on
   the load balancer address and cookies are not marked Secure.
3. Wait about 10–15 minutes (RDS and ElastiCache are slow). Open the `Url`
   output and sign in as `admin`; the password is in Secrets Manager (see the
   `AdminPasswordSecret` output).

Limits: one task (no horizontal scaling, a short blip on redeploy); installed
plugins live on the task's ephemeral disk and are lost when it restarts;
images come from Docker Hub, which rate-limits anonymous pulls from shared AWS
addresses; keep the `EncryptionKeySecret` safe. The bucket and database
snapshot are kept when you delete the stack.

## GCP lightweight (managed services, no Agent Runner)

The button opens Cloud Shell with
[`deploy/cloud/gcp/tutorial-lite.md`](../../deploy/cloud/gcp/tutorial-lite.md),
which runs [`deploy-lite.sh`](../../deploy/cloud/gcp/deploy-lite.sh). It creates
**one Cloud Run service** (gateway, API, web and realtime as containers of the
same instance), **Cloud SQL** Postgres, **Memorystore** Redis, a **Cloud
Storage** bucket (S3-compatible access through an HMAC key) and Secret Manager
secrets. Docker Hub images are pulled through an Artifact Registry remote
repository. Allow about 15 minutes; the script can be re-run safely.

Limits: one always-on instance (no scaling out); installed plugins live on the
instance's ephemeral disk; Memorystore and Cloud SQL bill continuously even
when idle. Storage through Cloud Storage's S3 interoperability endpoint and the
multi-container Cloud Run spec are untested against a live project.

## DigitalOcean App Platform (no Agent Runner)

[`.do/deploy.template.yaml`](../../.do/deploy.template.yaml) deploys the API and
realtime services plus a managed Postgres dev database. The React app is plain
static files, so it runs as a free App Platform **static site** (built from
`master`, so it can be slightly ahead of the `:latest` API image between
releases). App Platform's router replaces Caddy. There are no persistent
volumes, and dev databases are PostgreSQL-only, so before clicking:

1. Create a Space (e.g. `my-paca-files`) in `nyc3` and a Spaces access key.
2. Create a Valkey/Redis instance (DigitalOcean Managed Valkey, Upstash, …) and
   copy its `rediss://` URL.
3. Click the button and fill the secret fields: `JWT_SECRET`, `ADMIN_PASSWORD`,
   `AI_AGENT_INTERNAL_KEY`, `ENCRYPTION_KEY` (`openssl rand -hex 32`),
   `REDIS_URL` (for both `api` and `realtime`), the Spaces key/secret, and
   `STORAGE_BUCKET`.
4. Add a CORS rule on the Space allowing your app URL (GET/PUT/HEAD), since
   browsers upload via presigned Spaces URLs.

If your Space is not in `nyc3`, change `STORAGE_ENDPOINT`/`STORAGE_REGION`.

## DigitalOcean (Droplet)

1. Create an **Ubuntu 24.04** Droplet, **4 GB RAM / 2 vCPU or more**.
2. Under *Advanced Options → Add Initialization scripts*, paste
   [`deploy/cloud/digitalocean-cloud-init.yaml`](../../deploy/cloud/digitalocean-cloud-init.yaml).
   Fill in `PACA_DOMAIN` (DNS already pointing at the Droplet → HTTPS) and/or
   `ADMIN_PASSWORD`, or leave both empty.
3. After ~5 minutes open `http://<droplet-ip>`. Admin credentials:
   `ssh root@<ip> grep ADMIN_ /opt/paca/.env` (progress: `/var/log/paca-install.log`).

**True one-click (maintainers).** Once Paca is listed as a DigitalOcean
Marketplace 1-Click App, the button can create the Droplet directly. The Packer
template, build steps and submission checklist are in
[`deploy/cloud/digitalocean/marketplace/`](../../deploy/cloud/digitalocean/marketplace/README.md).
Until the listing is approved, the button opens this guide.

## Render (Blueprint)

[![Deploy to Render](https://render.com/images/deploy-to-render-button.svg)](https://render.com/deploy?repo=https://github.com/Paca-AI/paca)

[`render.yaml`](../../render.yaml) provisions Postgres, Key Value (Valkey),
RustFS (with a disk), the API and realtime services and a Caddy gateway
(the public entrypoint). The React app is a free Render **static site** built
from this repo; everything else is a paid starter plan.

`ENCRYPTION_KEY` (64 hex chars, `openssl rand -hex 32`) is prompted for on
create; keep it, since changing it makes stored secrets unreadable.

`ENCRYPTION_KEY` (64 hex chars, `openssl rand -hex 32`) is prompted for on
create; keep it, since changing it makes stored secrets unreadable.

After the first deploy, Render can't know the public URL ahead of time, so set:

- `paca-api`: `PUBLIC_URL=https://<gateway>.onrender.com`,
  `STORAGE_PUBLIC_URL=https://<gateway>.onrender.com/storage`
- `paca-realtime`: `CORS_ORIGINS=https://<gateway>.onrender.com`
- `paca` (gateway): `WEB_UPSTREAM=https://<paca-web>.onrender.com`, the static
  site's URL (the gateway proxies the app to it)

then redeploy those two services. The generated `ADMIN_PASSWORD` is in
`paca-api` → Environment.

## Railway (manual template)

Railway templates are authored in the Railway dashboard (there is no in-repo
file format that creates one), so the "Deploy on Railway" button needs a
maintainer to build the template once. Recipe, mirroring the Render blueprint:

1. New project → add **PostgreSQL** and **Redis** (or Valkey) from the catalog.
2. Add Docker-image services: `pacaai/paca-api`, `pacaai/paca-web`,
   `pacaai/paca-realtime`, `rustfs/rustfs:1.0.0-rc.6` (volume at `/data/rustfs0`).
3. Add a service built from this repo with Dockerfile path
   `deploy/render/gateway/Dockerfile` (it's a plain Caddy image; Railway
   handles TLS). Give it the only public domain and set `PORT=80`,
   `SITE_ADDRESS=:80`, plus `API_UPSTREAM`, `WEB_UPSTREAM`,
   `REALTIME_UPSTREAM`, `STORAGE_UPSTREAM` as `<service>.railway.internal:<port>`
   (8080, 3000, 3001, 9000).
4. API env: copy the keys from `render.yaml`'s `paca-api` block, using
   `${{Postgres.DATABASE_URL}}` / `${{Redis.REDIS_URL}}` references,
   `PUBLIC_URL=https://${{gateway.RAILWAY_PUBLIC_DOMAIN}}` and
   `STORAGE_PUBLIC_URL=https://${{gateway.RAILWAY_PUBLIC_DOMAIN}}/storage`.
   Realtime: `API_URL=http://paca-api.railway.internal:8080`, `REDIS_URL`,
   `CORS_ORIGINS=https://${{gateway.RAILWAY_PUBLIC_DOMAIN}}`.
   Use `${{secret(32)}}` for `JWT_SECRET`, `ADMIN_PASSWORD` and
   `AI_AGENT_INTERNAL_KEY` (the API refuses to start without it), and
   `${{secret(64, "abcdef0123456789")}}` for `ENCRYPTION_KEY`.
5. Publish via *Settings → Generate Template*, then add the resulting
   `https://railway.com/deploy/<code>` button to the README.

## Maintainer setup for the AWS and Railway buttons

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

**Railway button:** follow the Railway recipe above, publish the template
(*Settings → Generate Template*), then change the Railway README button to
`https://railway.com/deploy/<code>`.
