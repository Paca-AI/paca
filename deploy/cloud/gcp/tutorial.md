# Deploy Paca on Google Cloud

## Choose a project

Pick (or create) a project with billing enabled:

```bash
gcloud config set project <PROJECT_ID>
```

## Deploy

This creates a static IP, a firewall rule for ports 80/443, and an Ubuntu
24.04 VM (e2-standard-2, 40 GB) that installs the full Paca stack, including
the Agent Runner, on first boot.

```bash
bash deploy/cloud/gcp/deploy.sh
```

Optional: set `PACA_DOMAIN=paca.example.com` (DNS pointed at the reserved IP,
for HTTPS), `ADMIN_PASSWORD=...`, `PACA_ZONE=...` or `PACA_MACHINE=...` in
front of the command.

## Open Paca

After about 5 minutes open the URL the script printed. Read the admin
credentials with the command it printed.
