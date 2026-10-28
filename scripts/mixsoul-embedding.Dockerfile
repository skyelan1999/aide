FROM python:3.12-slim@sha256:a0f57064cbdcd6285fa45f51237ab0b35ea8ebaed06a1f5973e89e19dfb86704
ENV HF_HUB_OFFLINE=1 TRANSFORMERS_OFFLINE=1 HF_HOME=/tmp/hf TOKENIZERS_PARALLELISM=false
RUN pip install --no-cache-dir torch==2.14.1 --index-url https://download.pytorch.org/whl/cpu
RUN pip install --no-cache-dir sentence-transformers==6.2.0 transformers==5.20.2
COPY scripts/mixsoul-embedding-service.py /app/service.py
USER 1000:1000
ENTRYPOINT ["python", "-u", "/app/service.py"]
