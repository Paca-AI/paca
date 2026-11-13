# Deploy Paca (lightweight) on Google Cloud

Managed services only, no VM: Cloud Run, Cloud SQL, Memorystore and Cloud
Storage. It does **not** include the Agent Runner (AI agent sandboxes); use the
full-stack tutorial for that.

## Choose a project

Pick (or create) a project with billing enabled:

```bash
gcloud config set project <PROJECT_ID>
```

## Deploy

Creating the database and cache takes about 15 minutes in total. The script is
safe to re-run if it stops partway.

```bash
bash deploy/cloud/gcp/deploy-lite.sh
```

Optional: set `PACA_REGION=...` or `PACA_VERSION=...` in front of the command.

## Open Paca

Open the URL the script printed and sign in as `admin` with the password from
the command it printed.
