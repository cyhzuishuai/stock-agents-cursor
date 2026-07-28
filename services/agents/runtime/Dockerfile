FROM python:3.12-slim

WORKDIR /app

COPY packages/contracts /app/packages/contracts
COPY services/agents/common /app/services/agents/common
COPY services/agents/runtime /app/services/agents/runtime

# Avoid editable installs (setuptools build isolation needs PyPI).
# Install runtime deps only; put packages on PYTHONPATH.
ENV PYTHONPATH=/app/services/agents/common:/app/services/agents/runtime
RUN pip install --no-cache-dir \
      --trusted-host pypi.org \
      --trusted-host files.pythonhosted.org \
      --trusted-host pypi.tuna.tsinghua.edu.cn \
      -i https://pypi.tuna.tsinghua.edu.cn/simple \
      -r /app/services/agents/runtime/requirements.txt \
      fastapi 'uvicorn[standard]' httpx jsonschema

WORKDIR /app/services/agents/runtime

EXPOSE 8001

CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "8001"]
