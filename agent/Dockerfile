# 独立 AI Agent 服务（agentd）镜像。
#
# 构建（在仓库根目录执行，需先跑 ./build-agent.sh linux 生成二进制）：
#   docker build -f agent/Dockerfile -t apitool-agent .
#
# 运行：
#   docker run -d --name agent -p 8090:8090 \
#     -e AGENT_TOKEN=$(openssl rand -hex 16) \
#     -v $PWD/agent-data:/data \
#     apitool-agent

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app
COPY dist/agentd-linux-amd64 /app/agentd

# 数据目录（SQLite 与会话记录）
VOLUME ["/data"]

ENV AGENT_ADDR=:8090 \
    AGENT_DATA=/data \
    AGENT_DB=sqlite

EXPOSE 8090
ENTRYPOINT ["/app/agentd"]
