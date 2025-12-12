# Portage Engine 部署指南

本文档说明如何将 Portage Engine 部署到远程服务器。

## 部署环境

- **目标服务器**: 10.42.0.1
- **SSH 用户**: chris
- **服务运行用户**: portage
- **部署节点**: Server + Dashboard + Builder (三合一节点)

## 前置要求

### 本地机器

1. 已安装 Go 1.21+ 编译环境
2. 可以通过 SSH 连接到目标服务器
3. SSH 密钥已配置（建议使用密钥认证）

```bash
# 测试 SSH 连接
ssh chris@10.42.0.1 "echo 'Connection OK'"
```

### 目标服务器 (10.42.0.1)

1. 操作系统: Linux (推荐 Gentoo 或其他主流发行版)
2. 用户 chris 具有 sudo 权限
3. 已安装 systemd
4. 防火墙开放端口: 8080 (Server), 3000 (Dashboard), 9090 (Builder)

## 快速部署

### 1. 更新配置文件

首先更新配置文件中的 IP 地址：

```bash
# 在项目根目录执行
chmod +x scripts/update-configs.sh
./scripts/update-configs.sh 10.42.0.1
```

这会自动更新以下配置：
- `configs/server.conf` - Server 配置
- `configs/dashboard.conf` - Dashboard 配置（添加 SERVER_URL）
- `configs/builder.conf` - Builder 配置（添加 SERVER_URL）

### 2. 执行部署

```bash
# 赋予执行权限
chmod +x scripts/deploy.sh

# 开始部署（使用默认参数）
./scripts/deploy.sh

# 或者指定参数
./scripts/deploy.sh --host 10.42.0.1 --user chris --service-user portage
```

部署脚本会自动执行以下步骤：
1. 检查 SSH 连接
2. 编译所有二进制文件
3. 创建远程目录结构
4. 上传二进制文件
5. 上传配置文件
6. 创建 systemd 服务
7. 启用并启动服务
8. 显示服务状态

### 3. 验证部署

部署完成后，访问以下地址验证：

- **Server API**: http://10.42.0.1:8080/health
- **Dashboard**: http://10.42.0.1:3000
- **Builder API**: http://10.42.0.1:9090/health

## 目录结构

部署后的远程服务器目录结构：

```
/opt/portage-engine/           # 安装目录
├── bin/                        # 二进制文件
│   ├── portage-server
│   ├── portage-dashboard
│   └── portage-builder
└── configs/                    # 配置文件副本（仅供参考）

/etc/portage-engine/            # 配置目录
├── server.conf
├── dashboard.conf
└── builder.conf

/var/lib/portage-engine/        # 数据目录
├── server/
├── dashboard/
└── builder/

/var/log/portage-engine/        # 日志目录
├── server.log
├── dashboard.log
└── builder.log

/var/cache/binpkgs/             # 二进制包缓存

/var/tmp/portage-builds/        # Builder 工作目录
/var/tmp/portage-artifacts/     # Builder 构建产物
```

## 服务管理

### 远程管理（推荐）

部署脚本会自动上传管理工具到远程服务器：

```bash
# SSH 到远程服务器
ssh chris@10.42.0.1

# 使用管理工具
sudo portage-ctl status        # 查看状态
sudo portage-ctl start          # 启动所有服务
sudo portage-ctl stop           # 停止所有服务
sudo portage-ctl restart        # 重启所有服务
sudo portage-ctl logs           # 查看 server 日志
sudo portage-ctl logs portage-builder  # 查看 builder 日志
sudo portage-ctl urls           # 显示服务 URL
```

### 使用 systemctl 命令

```bash
# 查看状态
sudo systemctl status portage-server
sudo systemctl status portage-dashboard
sudo systemctl status portage-builder

# 启动服务
sudo systemctl start portage-server
sudo systemctl start portage-dashboard
sudo systemctl start portage-builder

# 停止服务
sudo systemctl stop portage-{server,dashboard,builder}

# 重启服务
sudo systemctl restart portage-{server,dashboard,builder}

# 查看日志
sudo journalctl -u portage-server -f
sudo journalctl -u portage-dashboard -f
sudo journalctl -u portage-builder -f
```

### 开机自启

服务已配置为开机自启动，如需禁用：

```bash
sudo systemctl disable portage-{server,dashboard,builder}
```

## 更新部署

当代码更新后，重新部署：

```bash
# 1. 拉取最新代码
git pull

# 2. 更新配置（如果有变化）
./scripts/update-configs.sh 10.42.0.1

# 3. 重新部署
./scripts/deploy.sh

# 服务会自动重启
```

## 回滚和卸载

### 回滚到之前版本

如果新版本有问题，可以回滚：

```bash
# 运行回滚脚本
chmod +x scripts/rollback.sh
./scripts/rollback.sh
```

回滚选项：
1. 仅停止服务
2. 停止并禁用服务
3. 删除 systemd 文件
4. 删除二进制文件（保留配置和数据）
5. 完全清理（删除所有文件，保留 binpkg 缓存）
6. 删除服务用户
7. 完全卸载（以上全部）

### 完全卸载

```bash
# 在本地执行回滚脚本，选择选项 7
./scripts/rollback.sh

# 或者直接在远程服务器执行
ssh chris@10.42.0.1 "sudo systemctl stop portage-{server,dashboard,builder} && \
  sudo systemctl disable portage-{server,dashboard,builder} && \
  sudo rm -rf /opt/portage-engine /etc/portage-engine /var/lib/portage-engine /var/log/portage-engine && \
  sudo userdel portage"
```

## 配置说明

### Server 配置 (server.conf)

主要配置项：
```bash
SERVER_PORT=8080                      # API 端口
BINPKG_PATH=/var/cache/binpkgs        # 二进制包路径
MAX_WORKERS=5                          # 最大工作线程
STORAGE_TYPE=local                     # 存储类型
GPG_ENABLED=false                      # GPG 签名
```

### Dashboard 配置 (dashboard.conf)

主要配置项：
```bash
DASHBOARD_PORT=3000                    # Dashboard 端口
SERVER_URL=http://10.42.0.1:8080      # Server API 地址
AUTH_ENABLED=false                     # 认证
```

### Builder 配置 (builder.conf)

主要配置项：
```bash
BUILDER_PORT=9090                      # Builder 端口
BUILDER_WORKERS=2                      # 构建工作线程
USE_DOCKER=true                        # 使用 Docker 构建
SERVER_URL=http://10.42.0.1:8080      # Server API 地址
```

## 监控和日志

### 查看日志

```bash
# 实时查看所有服务日志
sudo journalctl -u portage-server -u portage-dashboard -u portage-builder -f

# 查看最近的错误
sudo journalctl -u portage-server -p err -n 50

# 查看特定时间范围的日志
sudo journalctl -u portage-server --since "1 hour ago"
```

### 日志文件

应用日志也会写入文件：
```bash
tail -f /var/log/portage-engine/server.log
tail -f /var/log/portage-engine/dashboard.log
tail -f /var/log/portage-engine/builder.log
```

## 故障排查

### 服务无法启动

1. 检查日志：
```bash
sudo journalctl -u portage-server -n 100 --no-pager
```

2. 检查端口占用：
```bash
sudo netstat -tlnp | grep -E '8080|3000|9090'
```

3. 检查配置文件：
```bash
cat /etc/portage-engine/server.conf
```

### 权限问题

```bash
# 重新设置权限
sudo chown -R portage:portage /var/lib/portage-engine
sudo chown -R portage:portage /var/log/portage-engine
sudo chown -R portage:portage /var/cache/binpkgs
```

### 连接问题

```bash
# 检查防火墙
sudo iptables -L -n | grep -E '8080|3000|9090'

# 或者使用 firewalld
sudo firewall-cmd --list-ports
```

## 高级配置

### 启用 GPG 签名

1. 在远程服务器生成 GPG 密钥：
```bash
ssh chris@10.42.0.1
sudo su - portage
gpg --gen-key
```

2. 更新配置文件：
```bash
# /etc/portage-engine/server.conf
GPG_ENABLED=true
GPG_KEY_ID=your-key-id
GPG_KEY_PATH=/var/lib/portage-engine/gpg-key.asc

# /etc/portage-engine/builder.conf
GPG_ENABLED=true
GPG_KEY_ID=your-key-id
GPG_KEY_PATH=/var/lib/portage-engine/gpg-key.asc
```

3. 重启服务：
```bash
sudo systemctl restart portage-{server,builder}
```

### 使用 S3 存储

更新配置启用 S3：
```bash
# /etc/portage-engine/server.conf
STORAGE_TYPE=s3
STORAGE_S3_BUCKET=my-binpkgs
STORAGE_S3_REGION=us-east-1
AWS_ACCESS_KEY_ID=your-access-key
AWS_SECRET_ACCESS_KEY=your-secret-key
```

## 安全建议

1. **使用防火墙**：仅开放必要的端口
2. **启用 HTTPS**：使用反向代理（如 Nginx）配置 SSL
3. **定期备份**：备份 `/var/lib/portage-engine` 和 `/etc/portage-engine`
4. **监控日志**：设置日志告警
5. **更新系统**：定期更新系统和依赖

## 支持

如有问题，请查看：
- 日志文件：`/var/log/portage-engine/`
- 系统日志：`sudo journalctl -u portage-*`
- 项目文档：README.md
