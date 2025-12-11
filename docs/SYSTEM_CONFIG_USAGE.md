# 使用系统 Portage 配置进行构建

## 概述

Portage Engine 现在支持直接从用户的 `/etc/portage` 目录读取配置，这确保了构建时 USE flags 和其他配置与用户系统完全一致。

## 为什么需要这个功能？

在 Gentoo 系统中，不同用户可能有不同的配置：
- 不同的 USE flags 组合
- 特定包的关键字接受（~amd64）
- 包的屏蔽和解除屏蔽
- 自定义的仓库配置
- make.conf 中的编译选项

直接读取系统配置可以：
✅ **保证一致性** - 构建的包与本地系统配置完全一致
✅ **避免冲突** - 不会因为 USE flags 不匹配导致依赖冲突
✅ **简化操作** - 无需手动维护配置文件
✅ **完整迁移** - 自动包含所有相关配置

## 支持的配置文件

系统会读取以下 Portage 配置：

### 1. make.conf
- 位置: `/etc/portage/make.conf` 或 `/etc/make.conf`
- 内容: 全局编译选项、USE flags、CFLAGS、MAKEOPTS 等

```bash
CFLAGS="-O2 -pipe -march=native"
MAKEOPTS="-j8"
USE="ssl threads sqlite readline"
```

### 2. package.use
- 位置: `/etc/portage/package.use` (文件或目录)
- 内容: 特定包的 USE flags

```bash
dev-lang/python ssl threads sqlite
dev-lang/python:3.11 -test -doc
app-editors/vim python vim-pager
```

### 3. package.accept_keywords
- 位置: `/etc/portage/package.accept_keywords`
- 内容: 接受测试版本的包

```bash
dev-lang/rust ~amd64
sys-devel/gcc ~amd64
```

### 4. package.mask
- 位置: `/etc/portage/package.mask`
- 内容: 屏蔽的包版本

```bash
>=dev-lang/python-3.12
```

### 5. package.unmask
- 位置: `/etc/portage/package.unmask`
- 内容: 解除屏蔽的包

```bash
=sys-kernel/gentoo-sources-6.6.0
```

### 6. repos.conf
- 位置: `/etc/portage/repos.conf/`
- 内容: 仓库配置

```ini
[gentoo]
location = /var/db/repos/gentoo
sync-type = git
sync-uri = https://github.com/gentoo-mirror/gentoo.git
priority = 100
```

## 使用方法

### 1. 命令行工具

#### 基础使用
```bash
# 使用系统配置构建单个包
portage-client -portage-dir=/etc/portage \
               -package=dev-lang/python:3.11 \
               -server=http://build-server:8080

# 生成配置包
portage-client -portage-dir=/etc/portage \
               -package=dev-lang/python:3.11 \
               -output=python-bundle.tar.gz
```

#### 指定自定义 Portage 目录
```bash
# 如果配置在非标准位置
portage-client -portage-dir=/custom/portage \
               -package=app-editors/vim
```

### 2. 代码中使用

```go
package main

import (
    "log"
    "github.com/slchris/portage-engine/internal/builder"
)

func main() {
    // 创建配置传输器
    transfer := builder.NewConfigTransfer("")

    // 从系统读取配置
    config, err := transfer.ReadSystemPortageConfig("/etc/portage")
    if err != nil {
        log.Fatal(err)
    }

    // 查看读取到的配置
    log.Printf("Found %d package.use entries", len(config.PackageUse))
    log.Printf("Found %d repos", len(config.Repos))
    log.Printf("Global USE flags: %v", config.GlobalUse)

    // 创建包规范
    packages := &builder.BuildPackageSpec{
        Packages: []builder.PackageSpec{
            {
                Atom:    "dev-lang/python:3.11",
                Version: "3.11.8",
            },
        },
    }

    // 创建配置包
    bundle, err := transfer.CreateConfigBundle(
        config,
        packages,
        builder.BundleMetadata{
            UserID:     "myuser",
            TargetArch: "amd64",
            Profile:    "default/linux/amd64/23.0",
            CreatedAt:  "2025-12-11T10:00:00Z",
        },
    )

    // 导出为 tar.gz
    err = transfer.ExportBundle(bundle, "my-bundle.tar.gz")
    if err != nil {
        log.Fatal(err)
    }
}
```

## 实际场景示例

### 场景 1: 构建与本地完全一致的包

```bash
# 你的系统上 Python 配置了特定的 USE flags
cat /etc/portage/package.use/python
# dev-lang/python ssl threads sqlite readline -tk

# 使用相同配置在构建服务器上构建
portage-client -portage-dir=/etc/portage \
               -package=dev-lang/python:3.11 \
               -version=3.11.8
```

### 场景 2: 批量构建多个包

```bash
# 创建包列表
cat > packages.json <<EOF
{
  "packages": [
    {"atom": "dev-lang/python:3.11"},
    {"atom": "dev-python/pip"},
    {"atom": "dev-python/setuptools"},
    {"atom": "dev-python/virtualenv"}
  ]
}
EOF

# 使用系统配置构建所有包
portage-client -portage-dir=/etc/portage \
               -config=packages.json \
               -output=python-stack-bundle.tar.gz
```

### 场景 3: 构建需要特定关键字的测试包

```bash
# 系统中已经接受了测试版本
cat /etc/portage/package.accept_keywords/rust
# dev-lang/rust ~amd64

# 直接使用系统配置构建
portage-client -portage-dir=/etc/portage \
               -package=dev-lang/rust \
               -version=1.75.0
```

## 工作流程

```
用户系统                     配置读取                    构建服务器
┌────────────┐              ┌──────────┐                ┌───────────┐
│/etc/portage│              │ Reader   │                │  Builder  │
│            │──读取配置───→│          │──传输配置────→│           │
│ make.conf  │              │ Analyze  │                │  Apply    │
│ package.*  │              │ Bundle   │                │  Build    │
│ repos.conf │              └──────────┘                │  Package  │
└────────────┘                                          └───────────┘
```

1. **读取配置**: 从 `/etc/portage` 读取所有相关配置文件
2. **解析配置**: 解析 make.conf, package.use 等文件内容
3. **创建配置包**: 将配置打包成 JSON 格式
4. **传输到服务器**: 发送配置包到构建服务器
5. **应用配置**: 构建服务器应用这些配置
6. **执行构建**: 使用完全一致的配置进行构建

## 配置优先级

当同时提供多个配置源时，优先级如下：

1. **命令行参数** (最高优先级)
   - `-use`, `-keywords` 等直接指定的参数

2. **系统 Portage 目录**
   - `-portage-dir` 指定的目录

3. **JSON 配置文件**
   - `-config` 指定的文件

4. **默认值** (最低优先级)

## 注意事项

### 权限要求
- 读取 `/etc/portage` 需要适当的文件权限
- 某些文件可能需要 root 权限

### 路径处理
- 支持绝对路径和相对路径
- 自动处理文件和目录两种格式（如 package.use）

### 错误处理
- 如果某个配置文件不存在，会跳过继续读取其他文件
- 至少需要能读取 make.conf 或部分配置文件

### 兼容性
- 支持 Portage 2.3+ 的配置格式
- 兼容目录和单文件两种配置方式

## 测试

运行测试以验证配置读取功能：

```bash
# 运行所有配置传输测试
go test ./internal/builder/... -v

# 只运行系统配置读取测试
go test ./internal/builder/... -v -run TestReadSystemPortageConfig

# 查看测试覆盖率
go test ./internal/builder/... -cover
```

## 故障排除

### 问题: 找不到配置目录
```
Error: portage directory not found: /etc/portage
```

**解决方案**:
- 确认系统是 Gentoo 或配置了 Portage
- 使用 `-portage-dir` 指定正确的路径

### 问题: 权限被拒绝
```
Error: failed to read make.conf: permission denied
```

**解决方案**:
- 使用 `sudo` 运行
- 或者调整文件权限

### 问题: 配置不完整
```
Warning: Some configuration files could not be read
```

**解决方案**:
- 这是正常的，会继续使用已读取的配置
- 检查是否需要的配置文件存在

## 更多信息

- [Portage Configuration Guide](https://wiki.gentoo.org/wiki/Handbook:AMD64/Working/Portage)
- [USE Flags Documentation](https://wiki.gentoo.org/wiki/USE_flag)
- [Package Keywords](https://wiki.gentoo.org/wiki//etc/portage/package.accept_keywords)
