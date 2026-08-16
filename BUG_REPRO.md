# Bug Reproduction

## 包的性质

当前 test_model_fix 保存的是被测模型修复后的结果源码，不是初始含 Bug 源码。要复现原始缺陷，必须检出下面固定的 parent SHA；不要在当前修复结果源码上期待重新出现修复前失败。生成系统使用的可信验证补丁和完整验证日志仅在本地留存，不提交到结果分支。

## 问题现象

同一航次的第二个订舱签收不了，请帮我修复。

一个已到港航次上有两个都完成舱单申报的订舱。第一个订舱签收正常。第二个订舱调用签收接口返回 200，但返回的签收单里 booking_id 是第一个订舱的、签收人也是第一个人；第二个订舱的状态一直停在 manifested，没有生成属于它自己的签收记录。换成不同航次就正常。

期望：签收按订舱各自生成签收单，两个订舱都要推进到 arrived；同一个订舱重复签收仍然幂等，返回它原来的签收单和原签收人。修复后请保证 go test -timeout=120s -count=1 ./... 全绿，不要修改或跳过测试。

## 含 Bug 版本

- 仓库：11DingKing/goZZ-03
- 仓库地址：https://github.com/11DingKing/goZZ-03.git
- parent SHA：78588079893da2fc6e2511951c7860abb46ebb2f

## 复现步骤

```bash
git clone -- https://github.com/11DingKing/goZZ-03.git bug-repro
cd bug-repro
git checkout --detach 78588079893da2fc6e2511951c7860abb46ebb2f
go test ./internal/arrival -run "^TestSignOffIsPerBookingOnSharedVoyage$" -count=1 -v
```

## 双架构完整错误信息

### linux/amd64

- 容器内复现预期退出码：1
- 容器内复现实际退出码：1

stdout：

```text
$ go test ./internal/arrival -run "^TestSignOffIsPerBookingOnSharedVoyage$" -count=1 -v
=== RUN   TestSignOffIsPerBookingOnSharedVoyage
    shared_voyage_signoff_test.go:44: second sign-off returned arrival for BK-1, want BK-2
--- FAIL: TestSignOffIsPerBookingOnSharedVoyage (0.00s)
FAIL
FAIL	arcticdispatch/internal/arrival	0.039s
FAIL

```

stderr：

```text
(empty)
```

### linux/arm64

- 容器内复现预期退出码：1
- 容器内复现实际退出码：1

stdout：

```text
$ go test ./internal/arrival -run "^TestSignOffIsPerBookingOnSharedVoyage$" -count=1 -v
=== RUN   TestSignOffIsPerBookingOnSharedVoyage
    shared_voyage_signoff_test.go:44: second sign-off returned arrival for BK-1, want BK-2
--- FAIL: TestSignOffIsPerBookingOnSharedVoyage (0.00s)
FAIL
FAIL	arcticdispatch/internal/arrival	0.002s
FAIL

```

stderr：

```text
(empty)
```

## 通过条件

定向测试通过：go test ./internal/arrival -run '^TestSignOffIsPerBookingOnSharedVoyage$' -count=1 -v
全量回归 go test -timeout=120s -count=1 ./... 通过，go build ./... 与 go vet ./... 通过
同航次两个订舱各得到独立签收单且都变为 arrived；重复签收返回原记录与原签收人；未申报订舱与未到港航次的拒绝行为保持不变
