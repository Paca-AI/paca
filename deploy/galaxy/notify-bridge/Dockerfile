# galaxy-notify-bridge — Paca events → Galaxy notification inbox (ADR-038 P3.1).
# Built by deploy/galaxy/docker-compose.galaxy.yml (service notify-bridge).
# The same image also serves the dock-trigger sibling (service dock-trigger,
# command ["python", "-u", "dock_trigger.py"]) — Paca events → ChatDock agent.
FROM python:3.12-slim

WORKDIR /app

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY main.py dock_trigger.py .

# No secrets baked in; everything arrives via environment at runtime.
USER nobody
CMD ["python", "-u", "main.py"]
