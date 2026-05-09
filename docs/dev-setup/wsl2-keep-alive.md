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

### 0.1 容器命名卷数据持久化（不自动启动，按需手动 start）

**4 容器统一约定**：

| 容器 | 端口 | 命名卷 → 容器内路径 | RestartPolicy |
|---|---|---|---|
| dm8-test | 5236 | `dm8-data` → `/opt/dmdbms/data` | `no` |
| mysql8 | 3306 | `mysql8-data` → `/var/lib/mysql` | `no` |
| pg16 | 5432 | `pg16-data` → `/var/lib/postgresql/data` | `no` |
| oracle-free | 1521 | `oracle-data` → `/opt/oracle/oradata` | `no` |

> **2026-05-09 决策修订**：原本 4 容器都设 `--restart=unless-stopped` 自动启动。但实际频率统计后改成 `no`：oracle 启动 5 分钟 + ~1.5GB RAM + 罕用，pg 中频，mysql/dm 高频但启动也快——**全部按需手动 `docker start`** 比自动起更可控、避免无谓资源占用。

**重建命令样例**（首次部署或彻底重建用，注意 `--restart=no` 显式声明默认策略）：

```bash
# 在 WSL 内（或 wsl -d Ubuntu-24.04 -e bash -c "..."）

# dm8-test（dameng 8 Oracle 兼容模式，自构建镜像）
docker run -d --name dm8-test --restart=no \
  -p 5236:5236 -v dm8-data:/opt/dmdbms/data gplus/dm8:8.1.4.200

# mysql8
docker run -d --name mysql8 --restart=no \
  -p 3306:3306 -e MYSQL_ROOT_PASSWORD=root -e MYSQL_DATABASE=test \
  -v mysql8-data:/var/lib/mysql mysql:8.0

# pg16
docker run -d --name pg16 --restart=no \
  -p 5432:5432 -e POSTGRES_PASSWORD=postgres \
  -v pg16-data:/var/lib/postgresql/data postgres:16

# oracle-free
docker run -d --name oracle-free --restart=no \
  -p 1521:1521 -e ORACLE_PASSWORD=oracle \
  -v oracle-data:/opt/oracle/oradata gvenzl/oracle-free:23-slim
```

> **docker named volume 自动 populate**：dm8 / oracle 这种镜像 build 时已写入数据目录的，docker 看到挂载的命名卷为空时会**自动从镜像层 populate**，不需要手动 cp。pg / mysql 镜像 build 时数据目录是空的，由 entry script 在容器首次启动时 init。

**修改既有容器的 restart 策略**（不重建）：

```bash
docker update --restart=no dm8-test mysql8 pg16 oracle-free      # 当前用此
docker update --restart=unless-stopped dm8-test mysql8           # 想改回自启用此
```

验证：`docker inspect <名> --format '{{.HostConfig.RestartPolicy.Name}}'` 应为 `no`。

**新流程**：每次 distro 启动 → systemd 起 dockerd → 容器都 Exited 状态 → **按需 `docker start <名>`**，详见 §2。

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

进入 bash 提示符（如 `u1851@DESKTOP-AND8JCN:~$`）。**这个 PowerShell 窗口最小化挂着别关**——只要它在，distro 就 Running。注意：**容器不会自动 Up**（4 容器都设 `--restart=no`），需要在 §2 按需手动 `docker start`。

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

## 2. 启动需要的容器（按需手动 docker start）

4 容器都设 `--restart=no`，distro 启动后**不会自动起**。按今天要做的事 start 对应容器：

```powershell
# 跑 mysql 测试时
wsl -d Ubuntu-24.04 -e docker start mysql8

# 跑 dm 测试时（启动慢需等）
wsl -d Ubuntu-24.04 -e bash -c "docker start dm8-test && until docker logs dm8-test 2>&1 | tail -50 | grep -q 'SYSTEM IS READY'; do sleep 2; done; echo dm8-test READY"

# 跑 pg 测试时
wsl -d Ubuntu-24.04 -e bash -c "docker start pg16 && until docker logs pg16 2>&1 | tail -20 | grep -q 'database system is ready to accept connections'; do sleep 2; done; echo pg16 READY"

# 跑 oracle 测试时（启动 ~5 分钟慢）
wsl -d Ubuntu-24.04 -e bash -c "docker start oracle-free && until docker logs oracle-free 2>&1 | tail -50 | grep -q 'DATABASE IS READY'; do sleep 5; done; echo oracle-free READY"
```

ready 时长参考：
- mysql8 / pg16：< 10 秒
- dm8-test：30 秒到 2 分钟
- oracle-free：约 5 分钟（首次更长，含 PDB 创建）

跑完测试想停容器释放资源：`wsl -d Ubuntu-24.04 -e docker stop <名>`。命名卷在 docker rm 之前不丢数据。

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

前提：4 容器已建好（命名卷模式），见 §0.1。容器都设 `--restart=no`，需手动 start。

```powershell
# start-dm-test.ps1
# 用法：powershell -ExecutionPolicy Bypass -File start-dm-test.ps1

# 1. 启 background wsl.exe 持有 distro（防 idle stop）
Start-Process wsl -ArgumentList '-d','Ubuntu-24.04','-e','sleep','infinity' -WindowStyle Hidden
Start-Sleep -Seconds 2
Write-Host "Background wsl.exe 已启动："
Get-Process wsl | Format-Table Id,ProcessName -AutoSize

# 2. start 需要的容器（按今天要做的事改这里——示例只起 dm8-test + mysql8）
wsl -d Ubuntu-24.04 -e bash -c "
  docker start dm8-test mysql8
  until docker logs dm8-test 2>&1 | tail -50 | grep -q 'SYSTEM IS READY'; do sleep 2; done
  echo 'dm8-test READY'
"

Write-Host ""
Write-Host "完成！现在可以跑测试："
Write-Host "  `$env:TEST_DM_DSN='dm://SYSDBA:Test_DM_2026@127.0.0.1:5236'"
Write-Host "  `$env:TEST_DM_REQUIRED='1'"
Write-Host "  go test -tags=dm -race -count=1 -v ./..."
```

清理对应：

```powershell
# stop-dm-test.ps1（核打击：关 distro + 所有容器）
wsl --shutdown
Write-Host "WSL 全部 distro 已 shutdown，所有 wsl.exe + 容器已停"
```

---

## 8. 相关参考

- 项目 README "DM 数据库支持" 章节 + "启动 DM 8 容器" 段警告框
- CHANGELOG v0.8.3 "已知部署陷阱" 段
- memory `wsl-distro-idle-stop.md`（含完整二轮实测路径与失败方案对照表）
- v0.8.3 commit 历史：`571696b`（首次错误根治宣言）→ `a5e4637`（修订）
