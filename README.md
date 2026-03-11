## 说明
makecli 是 make 平台管理的命令行工具

## 安装
```bash
brew tap MakeHQ/makecli
brew install makecli
```
## 强制升级到最新版本
```
git -C $(brew --repo makehq/makecli) pull && brew upgrade makecli
```
## 功能

### 配置凭证
```bash
# 配置默认 profile
makecli configure

# 配置指定 profile
makecli configure --profile todo
```

交互示例：
```
Configuring profile [default]
MakeHQ Access Token [****YDUW]:
Credentials saved to ~/.make/credentials
```

凭证保存在 `~/.make/credentials`，格式：
```ini
[default]
access_token = AKIAUXFQEUPWGEXEYDUW

[todo]
access_token = AKIAUXFQEUPWGEXEYDUW
```

### 查看版本
```bash
makecli version
```

## 开发指南
### 编译
```
make
```

### 单元测试
```
make test
```

### 安装本地
```
make local
```
