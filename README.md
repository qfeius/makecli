## 说明
makecli 是 make 平台管理的命令行工具

## 安装
```bash
brew tap MakeHQ/makecli
brew install makecli
```
## 强制升级到最新版本
```
makecli update
```
或者使用下面命令
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

### 列出 App
```bash
# 默认表格输出
makecli app list

# JSON 输出
makecli app list --output json

# 分页查看第 2 页，每页 10 条
makecli app list --page 2 --size 10
```

`app list` 支持的输出格式：

- `--output table` 默认表格输出
- `--output json` 输出 `data` 和 `pagination`，便于脚本或 AI Agent 消费
- 列表模式支持 `--page` 和 `--size`，其中 `--page` 默认从 `1` 开始

### 列出 Entity
```bash
# 列出指定 app 下的全部 entity（默认表格输出）
makecli entity --app TODO list

# 以 JSON 输出 entity 列表
makecli entity --app TODO list --output json

# 分页查看第 2 页，每页 10 条
makecli entity --app TODO list --page 2 --size 10

# 查看单个 entity 详情
makecli entity --app TODO list Task

# 以 JSON 输出单个 entity 详情
makecli entity --app TODO list Task --output json
```

`entity list` 支持的输出格式：

- `--output table` 默认表格/详情文本输出
- `--output json` 列表模式输出 `data` 和 `pagination`，详情模式输出单个 `data` 对象
- 列表模式支持 `--page` 和 `--size`，其中 `--page` 默认从 `1` 开始

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
