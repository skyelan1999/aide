# 仅供联调：把 scripts/mock_clone_server.py 打进一个最小镜像。
# 真实部署时不用这个镜像，换成你的 IndexTTS2/CosyVoice2/GPT-SoVITS 镜像。
FROM python:3.12-slim
WORKDIR /app
COPY mock_clone_server.py /app/mock_clone_server.py
EXPOSE 9880
CMD ["python3", "/app/mock_clone_server.py", "--port", "9880"]
