# WSL2 Keep-Alive 操作手册（dm8-test 等长跑容器）

> **场景**：在 WSL2 + Docker Engine 跑 dm8-test / pg16 / oracle-free 等长跑测试容器。WSL2 distro 在"无 wsl.exe 进程 attached"时约 60 秒后 auto stop，距离会拖死所有内部容器。
>
> **二轮实测无效方案（请勿浪费时间）**：
> - `.wslconfig` 设 `vmIdleTimeout=-1` 或 `vmIdleTimeout=4294967295`
> - distro 内 systemd `wsl-keep-alive.service` 跑 `sleep infinity`
>
> **唯一实测有效方案**：Windows 主机持续有一个 wsl.exe 进程 attached（详见下文 Workaround B）。

实测路径与决策详见 `memory/wsl-distro-idle-stop.md`。

---

## 1. 启动 keep-alive（开始测试前）

在 PowerShell 跑（不要在 Claude 对话框里跑——Claude 是非管理员杀不了它）：

```powershell
Start-Process wsl -ArgumentList '-d','Ubuntu-24.04','-e','sleep','infinity' -WindowStyle Hidden
```

**验证起来了**（应见至少 1 个 wsl 进程）：

```powershell
Get-Process wsl
```

期望输出类似：

```
Handles  NPM(K)    PM(K)      WS(K)     CPU(s)     Id  SI ProcessName
-------  ------    -----      -----     ------     --  -- -----------
    109       7     1224       7628       0.02   2824   7 wsl
    214      14     2676      13648       0.00  21508   7 wsl
```

CPU 接近 0 说明在 idle sleep 状态，符合预期。

---

## 2. 启动 dm8-test 容器

```powershell
wsl -d Ubuntu-24.04 -e bash -c "docker start dm8-test && until docker logs dm8-test 2>&1 | tail -50 | grep -q 'SYSTEM IS READY'; do sleep 2; done; echo READY"
```

DM 8 启动到 SYSTEM IS READY 大约 30 秒到 2 分钟。看到 `READY` 即可连接 `dm://SYSDBA:Test_DM_2026@127.0.0.1:5236`。

---

## 3. 跑 dm 测试（gplus 项目内）

```powershell
$env:TEST_DM_DSN="dm://SYSDBA:Test_DM_2026@127.0.0.1:5236"
$env:TEST_DM_REQUIRED="1"
go test -tags=dm -race -count=1 -v ./...
```

期望 9 个 dm 测试 17+ 子测试全过。

---

## 4. 验证 keep-alive 真生效（可选自检）

关闭当前 PowerShell 窗口 → 等 5-10 分钟（远超 60 秒 idle 阈值）→ 重新开 PowerShell：

```powershell
wsl -d Ubuntu-24.04 -e docker ps --filter name=dm8-test
```

期望 STATUS 仍是 `Up X minutes`（不是 `Exited`）。如果是 `Exited`，说明 background wsl.exe 没起或被杀了——回到第 1 步。

---

## 5. 清理（测试完毕）

**方案 A（推荐）：核打击式 wsl --shutdown**

```powershell
wsl --shutdown
```

效果：杀所有 wsl.exe 进程 + 关所有 distro 内的所有容器。简单粗暴。

**方案 B：精准杀 wsl.exe（需在你启动它的同一 PowerShell）**

```powershell
Get-Process wsl | Stop-Process -Force
```

注意：**只能杀同一用户、同一 process token 启动的进程**。如果 PowerShell A 启动的 background，PowerShell B 杀不了（Access Denied）。这就是为什么 Claude 对话框杀不掉用户自己 PowerShell 起的 wsl.exe。

---

## 6. 常见坑

| 坑 | 现象 | 解决 |
|---|---|---|
| 复制中文连词被 PowerShell 当参数 | `Start-Process : 无法绑定参数 WindowStyle ... Hidden，然后 ...` | 去掉中文"，然后"等连词，命令一行一行单独粘贴 |
| `Stop-Process Access Denied` | 杀不掉 background wsl.exe | 用 `wsl --shutdown` 替代，或用启动它的同一 PowerShell 跑 Stop-Process |
| dm8-test 启动 30 秒就 Exited | docker logs 看到 `Server is stopping ... shutdown successfully` graceful 序列 | distro idle stop 拖死，回到第 1 步起 keep-alive |
| `Test-NetConnection: command not found` | bash 里跑 PowerShell 命令 | 切到 PowerShell 跑，或用 `nc -zv 127.0.0.1 5236` |

---

## 7. 一次性快速启动脚本（可保存为 .ps1）

```powershell
# start-dm-test.ps1
# 用法：在 PowerShell 跑 powershell -ExecutionPolicy Bypass -File start-dm-test.ps1

Start-Process wsl -ArgumentList '-d','Ubuntu-24.04','-e','sleep','infinity' -WindowStyle Hidden
Start-Sleep -Seconds 2
Write-Host "Background wsl.exe 已启动，进程列表:"
Get-Process wsl | Format-Table Id,ProcessName -AutoSize

wsl -d Ubuntu-24.04 -e bash -c "docker start dm8-test && until docker logs dm8-test 2>&1 | tail -50 | grep -q 'SYSTEM IS READY'; do sleep 2; done; echo READY"

Write-Host ""
Write-Host "完成！现在可以跑测试："
Write-Host "  `$env:TEST_DM_DSN='dm://SYSDBA:Test_DM_2026@127.0.0.1:5236'"
Write-Host "  `$env:TEST_DM_REQUIRED='1'"
Write-Host "  go test -tags=dm -race -count=1 -v ./..."
```

清理对应：

```powershell
# stop-dm-test.ps1
wsl --shutdown
Write-Host "WSL 全部 distro 已 shutdown，所有 wsl.exe + 容器已停"
```

---

## 8. 相关参考

- 项目 README "DM 数据库支持" 章节 + "启动 DM 8 容器" 段警告框
- CHANGELOG v0.8.3 "已知部署陷阱" 段
- memory `wsl-distro-idle-stop.md`（含完整二轮实测路径与失败方案对照表）
- v0.8.3 commit 历史：`571696b`（首次错误根治宣言）→ `a5e4637`（修订）
