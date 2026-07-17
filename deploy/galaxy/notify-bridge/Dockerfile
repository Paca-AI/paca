# galaxy-notify-bridge — Paca events → Galaxy notification inbox (ADR-038 P3.1).
# Built by deploy/galaxy/docker-compose.galaxy.yml (service notify-bridge).
FROM python:3.12-slim

WORKDIR /app

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY main.py .

# No secrets baked in; everything arrives via environment at runtime.
USER nobody
CMD ["python", "-u", "main.py"]
