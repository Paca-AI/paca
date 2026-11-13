#!/usr/bin/env bash
# Publish the CloudFormation template to S3 by hand (backs the README's
# "Deploy to AWS" button). Uses whatever AWS credentials your shell already
# has (aws sso login, AWS_PROFILE, ...) — no keys are stored anywhere.
#
#   bash deploy/cloud/aws/publish-template.sh [version]   # default: dev
set -euo pipefail
BUCKET="${AWS_TEMPLATE_BUCKET:-paca-cloudformation}"
VERSION="${1:-dev}"
DIR="$(dirname "$0")"
for t in paca.cfn.yaml paca-lite.cfn.yaml; do
    aws s3 cp "$DIR/$t" "s3://${BUCKET}/${VERSION}/$t"
    aws s3 cp "$DIR/$t" "s3://${BUCKET}/latest/$t"
    echo "Published: https://${BUCKET}.s3.amazonaws.com/latest/$t"
done
