FROM python:3.12-slim

WORKDIR /app

COPY packages/contracts /app/packages/contracts
COPY services/agents/common /app/services/agents/common
COPY services/agents/research /app/services/agents/research

RUN pip install --no-cache-dir -e /app/services/agents/common \
    && pip install --no-cache-dir -r /app/services/agents/research/requirements.txt

WORKDIR /app/services/agents/research

EXPOSE 8002

CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "8002"]
