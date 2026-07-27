FROM python:3.12-slim

WORKDIR /app

COPY packages/contracts /app/packages/contracts
COPY services/agents/common /app/services/agents/common
COPY services/agents/decision /app/services/agents/decision

RUN pip install --no-cache-dir -e /app/services/agents/common \
    && pip install --no-cache-dir -r /app/services/agents/decision/requirements.txt

WORKDIR /app/services/agents/decision

EXPOSE 8003

CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "8003"]
