# 快速部署指南

## 一键部署到 10.42.0.1

### 第一步：更新配置

```bash
./scripts/update-configs.sh 10.42.0.1
```

### 第二步：执行部署

```bash
./scripts/deploy.sh
```

部署完成后，服务会自动启动在：
- Server:    http://10.42.0.1:8080
- Dashboard: http://10.42.0.1:3000
- Builder:   http://10.42.0.1:9090

### 第三步：验证部署

```bash
# 检查健康状态
curl http://10.42.0.1:8080/health
curl http://10.42.0.1:9090/health

# 访问 Dashboard
open http://10.42.0.1:3000
```

## 服务管理

SSH 到服务器后使用管理命令：

```bash
ssh chris@10.42.0.1

# 查看状态
sudo portage-ctl status

# 重启服务
sudo portage-ctl restart

# 查看日志
sudo portage-ctl logs portage-server
```

## 常用命令速查

| 命令 | 说明 |
|------|------|
| `sudo portage-ctl status` | 查看所有服务状态 |
| `sudo portage-ctl start` | 启动所有服务 |
| `sudo portage-ctl stop` | 停止所有服务 |
| `sudo portage-ctl restart` | 重启所有服务 |
| `sudo portage-ctl logs` | 查看 server 日志 |
| `sudo portage-ctl urls` | 显示服务 URL |
| `sudo systemctl status portage-server` | 查看 server 状态 |
| `sudo journalctl -u portage-server -f` | 实时查看 server 日志 |

## 更新部署

```bash
git pull
./scripts/update-configs.sh 10.42.0.1
./scripts/deploy.sh
```

## 回滚

```bash
./scripts/rollback.sh
```

## 完整文档

详细部署文档请参考：[docs/DEPLOYMENT.md](../docs/DEPLOYMENT.md)
