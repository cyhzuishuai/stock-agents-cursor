FROM python:3.12-slim

WORKDIR /app

COPY packages/contracts /app/packages/contracts
COPY services/agents/common /app/services/agents/common
COPY services/agents/runtime /app/services/agents/runtime

RUN pip install --no-cache-dir -e /app/services/agents/common \
    && pip install --no-cache-dir -r /app/services/agents/runtime/requirements.txt

WORKDIR /app/services/agents/runtime

EXPOSE 8001

CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "8001"]
