# BENZHI_README

## 项目说明

- 项目：11DingKing/goZZ-03
- 项目用途：Production-shaped Go backend for the 中欧北极快航 (China–Europe Arctic Express) route-dispatch centre. It coordinates cabin-slot allocation, booking, port-arrival appointments, manifest declaration and arrival sign-off across five chained flows:
- Go 工具链：`golang:1.26`
- 前端工具链：无

## 标准构建、运行和测试命令

进入容器后执行：

```bash
# 编译
cd '/app' && GOTOOLCHAIN=local go build ./...

# 启动
cd '/app' && GOTOOLCHAIN=local go run ./cmd/server

# 测试
cd '/app' && GOTOOLCHAIN=local go test ./...
```

## Docker 构建和进入容器

```bash
chmod +x build_benzhi_docker.sh
./build_benzhi_docker.sh benzhi-task-18-amd64 linux/amd64
./build_benzhi_docker.sh benzhi-task-18-arm64 linux/arm64
docker run -it benzhi-task-18-amd64:latest
docker run -it --platform linux/arm64 benzhi-task-18-arm64:latest
```

## 题目验证命令

1. 预期退出码 0：`go test ./internal/arrival -run "^TestSignOffIsPerBookingOnSharedVoyage$" -count=1 -v`
2. 预期退出码 0：`go test -buildvcs=false -count=1 ./...`
3. 预期退出码 0：`GOTOOLCHAIN=local go build -buildvcs=false ./... && GOTOOLCHAIN=local go vet ./...`

## Bug 复现

Bug 现象、触发步骤和完整错误信息见 `BUG_REPRO.md`。
