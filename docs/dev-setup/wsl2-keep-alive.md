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

## 0. 一次性配置（已做过的不用重复）

### 0.1 容器自动 start + 命名卷数据持久化

**4 容器统一约定**（已在本机配置，下游可参考）：

| 容器 | 端口 | 命名卷 → 容器内路径 | RestartPolicy |
|---|---|---|---|
| dm8-test | 5236 | `dm8-data` → `/opt/dmdbms/data` | unless-stopped |
| mysql8 | 3306 | `mysql8-data` → `/var/lib/mysql` | unless-stopped |
| pg16 | 5432 | `pg16-data` → `/var/lib/postgresql/data` | unless-stopped |
| oracle-free | 1521 | `oracle-data` → `/opt/oracle/oradata` | unless-stopped |

**重建命令样例**（首次部署或彻底重建用）：

```bash
# 在 WSL 内（或 wsl -d Ubuntu-24.04 -e bash -c "..."）

# dm8-test（dameng 8 Oracle 兼容模式，自构建镜像）
docker run -d --name dm8-test --restart=unless-stopped \
  -p 5236:5236 -v dm8-data:/opt/dmdbms/data gplus/dm8:8.1.4.200

# mysql8
docker run -d --name mysql8 --restart=unless-stopped \
  -p 3306:3306 -e MYSQL_ROOT_PASSWORD=root -e MYSQL_DATABASE=test \
  -v mysql8-data:/var/lib/mysql mysql:8.0

# pg16
docker run -d --name pg16 --restart=unless-stopped \
  -p 5432:5432 -e POSTGRES_PASSWORD=postgres \
  -v pg16-data:/var/lib/postgresql/data postgres:16

# oracle-free
docker run -d --name oracle-free --restart=unless-stopped \
  -p 1521:1521 -e ORACLE_PASSWORD=oracle \
  -v oracle-data:/opt/oracle/oradata gvenzl/oracle-free:23-slim
```

> **docker named volume 自动 populate**：dm8 / oracle 这种镜像 build 时已写入数据目录的，docker 看到挂载的命名卷为空时会**自动从镜像层 populate**，不需要手动 cp。pg / mysql 镜像 build 时数据目录是空的，由 entry script 在容器首次启动时 init。

**给已存在容器加 restart 策略**（不重建，仅修改）：

```bash
docker update --restart=unless-stopped dm8-test mysql8 pg16 oracle-free
```

验证：`docker inspect <名> --format '{{.HostConfig.RestartPolicy.Name}}'` 应为 `unless-stopped`。

设了之后，每次 distro 启动 → systemd 自动起 dockerd → dockerd 自动 start 这些容器，**不用再手动 `docker start`**。

### 0.2 Windows 主机 MySQL 已停（避免端口冲突）

mysql8 容器映射 `-p 3306:3306` 与 Windows 主机 MySQL 服务冲突——已在管理员 PowerShell 跑：

```powershell
# 管理员 PowerShell（普通权限会 Access Denied）
Stop-Service MySQL
Set-Service MySQL -StartupType Manual
```

Windows MySQL 数据保留在 `D:\Environment\database\mysql\mysql-8.0.22-winx64\`，万一需要回退：管理员 `Set-Service MySQL -StartupType Automatic; Start-Service MySQL`，但需先 `docker stop mysql8` 释放 3306。

### 0.3 mysql8 容器初始化命令（已建，不用重跑）

```powershell
wsl -d Ubuntu-24.04 -e bash -c "docker run -d --name mysql8 --restart=unless-stopped -p 3306:3306 -e MYSQL_ROOT_PASSWORD=root -e MYSQL_DATABASE=test -v mysql8-data:/var/lib/mysql mysql:8.0"
```

数据卷 `mysql8-data`（docker named volume，跨容器重建持久化）。`test` 库自动建好供 gplus DSN 使用。

---

## 1. 启动 keep-alive（每次开机第一步）

两种方案，按场景选：

### 方案 A：交互式 `wsl`（最简单，推荐）

在普通 PowerShell（不需要 admin）跑：

```powershell
wsl
```

进入 bash 提示符（如 `u1851@DESKTOP-AND8JCN:~$`）。**这个 PowerShell 窗口最小化挂着别关**——只要它在，distro 就 Running，dm8-test 就自动 Up。

优劣：可见、心理踏实、可在里面直接跑 docker 命令；但占一个可见窗口，误关 / `exit` / Ctrl+D 会退。

> **管理员权限不必要**：keep-alive 只看 Windows 主机有没有 wsl.exe 进程 attached，跟权限无关。普通 PowerShell 跑 `wsl` 同样有效。
>
> 管理员权限的唯一好处是能用 `Stop-Process -Force` 杀别人启动的 wsl 进程（譬如方案 B 的 background 进程，普通 PowerShell 杀不动会报 Access Denied）。如果只是想关掉自己交互式 wsl，直接在 bash 里 `exit` 或关 PowerShell 窗口即可，不需要 admin。

### 方案 B：隐藏 background 进程

```powershell
Start-Process wsl -ArgumentList '-d','Ubuntu-24.04','-e','sleep','infinity' -WindowStyle Hidden
```

**验证起来了**（应见至少 1 个 wsl 进程，CPU 接近 0 说明 idle sleep）：

```powershell
Get-Process wsl
```

优劣：进程隐藏不占可见窗口、不会误关；但看不见、清理需 `wsl --shutdown` 或 admin Stop-Process。

---

## 2. 等 dm8-test SYSTEM IS READY

dockerd 自动 start 容器后，DM 8 内部启动到 SYSTEM IS READY 约 30 秒到 2 分钟。一条命令等就绪：

```powershell
wsl -d Ubuntu-24.04 -e bash -c "until docker logs dm8-test 2>&1 | tail -50 | grep -q 'SYSTEM IS READY'; do sleep 2; done; echo READY"
```

看到 `READY` 即可连接 `dm://SYSDBA:Test_DM_2026@127.0.0.1:5236`。

> 如果忘了配 `--restart=unless-stopped`（第 0 节），这一步前先手动 `wsl -d Ubuntu-24.04 -e docker start dm8-test`。

---

## 3. 跑 gplus 测试（项目内）

### dm 测试（DM 8）

```powershell
$env:TEST_DM_DSN="dm://SYSDBA:Test_DM_2026@127.0.0.1:5236"
$env:TEST_DM_REQUIRED="1"
go test -tags=dm -race -count=1 -v ./...
```

期望 9 个 dm 测试 17+ 子测试全过。

### mysql 测试

```powershell
$env:TEST_MYSQL_DSN="root:root@tcp(127.0.0.1:3306)/test?charset=utf8mb4&parseTime=True&loc=Local"
go test -race -count=1 -run "^TestMySQL_" -v ./...
```

不设 `TEST_MYSQL_DSN` 也能用——`defaultMySQLDSN` 默认就是这条，会被自动用。

### 三方言一起（默认 sqlite + mysql + dm）

```powershell
$env:TEST_MYSQL_DSN="root:root@tcp(127.0.0.1:3306)/test?charset=utf8mb4&parseTime=True&loc=Local"
$env:TEST_DM_DSN="dm://SYSDBA:Test_DM_2026@127.0.0.1:5236"
$env:TEST_DM_REQUIRED="1"
go test -tags=dm -race -count=1 ./...
```

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

前提：第 0 节的 `docker update --restart=unless-stopped dm8-test` 已配置过。

```powershell
# start-dm-test.ps1
# 用法：在 PowerShell 跑 powershell -ExecutionPolicy Bypass -File start-dm-test.ps1

Start-Process wsl -ArgumentList '-d','Ubuntu-24.04','-e','sleep','infinity' -WindowStyle Hidden
Start-Sleep -Seconds 2
Write-Host "Background wsl.exe 已启动（distro Running，dockerd 自动 start dm8-test）："
Get-Process wsl | Format-Table Id,ProcessName -AutoSize

Write-Host "等 dm8-test SYSTEM IS READY..."
wsl -d Ubuntu-24.04 -e bash -c "until docker logs dm8-test 2>&1 | tail -50 | grep -q 'SYSTEM IS READY'; do sleep 2; done; echo READY"

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
