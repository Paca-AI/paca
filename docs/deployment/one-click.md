# One-click deploys

| Provider | Path | Full stack (AI agents)? | Status |
|:--|:--|:--|:--|
| GCP | Cloud Shell tutorial + `gcloud` script (Compute Engine VM) | ✅ yes | Button added, untested |
| DigitalOcean | App Platform button (`.do/deploy.template.yaml`) | ❌ no Agent Runner | Button added, untested |
| DigitalOcean | Droplet + cloud-init | ✅ yes | Manual, no button |
| AWS | CloudFormation (EC2 + Elastic IP) | ✅ yes | Button needs the S3 setup below |
| Render | Blueprint (`render.yaml`) | ❌ no Agent Runner | Ready |
| Railway | Template (built in Railway dashboard) | ❌ no Agent Runner | Recipe only |

**Why only the VM paths run AI agents.** The Agent Runner starts each agent
sandbox as a Docker container through the host's Docker socket. Render and
Railway don't expose one, so there Paca runs without agent execution
(boards, docs, sprints, automations and realtime all work). Pick a VM deploy
if you need agents.

Nothing here has been run against a live account yet — treat the first
deploy on each provider as a test.

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

## DigitalOcean (App Platform button)

[`.do/deploy.template.yaml`](../../.do/deploy.template.yaml) deploys the API, web
and realtime services plus managed Postgres and Valkey, and uses App Platform's
router instead of Caddy. There are no persistent volumes, so files go to
**DigitalOcean Spaces**:

1. Create a Space (e.g. `my-paca-files`) in `nyc3` and a Spaces access key.
2. Click the button, fill the secret fields (`JWT_SECRET`, `ADMIN_PASSWORD`,
   `INTERNAL_API_KEY`, Spaces key/secret) and set `STORAGE_BUCKET`.
3. Add a CORS rule on the Space allowing your app URL (GET/PUT/HEAD), since
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

## Render (Blueprint)

[![Deploy to Render](https://render.com/images/deploy-to-render-button.svg)](https://render.com/deploy?repo=https://github.com/Paca-AI/paca)

[`render.yaml`](../../render.yaml) provisions Postgres, Key Value (Valkey),
RustFS (with a disk), the API, web, realtime services and a Caddy gateway
(the only public service). All are paid starter plans.

After the first deploy, Render can't know the public URL ahead of time, so set:

- `paca-api`: `PUBLIC_URL=https://<gateway>.onrender.com`,
  `STORAGE_PUBLIC_URL=https://<gateway>.onrender.com/storage`
- `paca-realtime`: `CORS_ORIGINS=https://<gateway>.onrender.com`

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
   Use `${{secret(32)}}` for `JWT_SECRET` and `ADMIN_PASSWORD`.
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

**Railway button:** follow the recipe above, then replace the "Railway button:
coming" line in the README with the `https://railway.com/deploy/<code>` button.
