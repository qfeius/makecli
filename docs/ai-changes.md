## 2026-11-11 DateTime 聚合 CLI 错误提示
- `record aggregate --help` 明确 DateTime 在同一分组项传粒度，示例按 `orderTime` 取月。组合规则由 Data 服务校验。
- `listPage` 按业务码解码响应，成功读取行数组，失败沿用 stderr 和非零退出码并保留后端错误位置、描述与建议。
- 缺少粒度和非法粒度用例先失败后通过。Go 全量测试、vet、二进制帮助检查及 npm 测试通过。

## 2026-11-10 DateTime 聚合帮助
- `record aggregate --help` 说明 DateTime 保持 `aggregable=false`，分桶必须指定 `granularity`。Date 粒度可选，示例使用 `orderTime` 按月分组。
- 同步 `GroupField` 注释和命令目录说明，沿用现有请求参数透传。
- CLI 聚合专项测试通过，用锁定的 Skill 快照构建二进制并验证实际帮助与示例 JSON。

## 2026-10-26：TC-013 初始化期间构建日志漏读回归
- 摘要：先保存构建起点，再扫描部署历史，防止期间新增构建失败被 tail=0 跳过。
- 涉及文件：logs_stages、logs_follow_start_test、帮助、README及模块地图；详见 ai-changes-app-build-logs.md。
- 决策：保持各流独立初始快照和既有部署历史过滤语义，后续按游标读取；不依赖任务阶段猜测历史。
- 验证：两例先红后绿，专项竞态20轮及全量竞态、vet/build、npm、lint通过；独立复审通过。

## 2026-10-26：TC-013 阶段跟随游标初始化
- 摘要：all + tail=0 + follow 在初始化阶段建立部署读取起点，阶段切换后不再过滤新事件。
- 涉及模块：logs_stages、HTTP 回归测试、命令帮助及文档；详细记录见 ai-changes-app-build-logs.md。
- 决策：复用现有分页与错误检查，只调整零历史组合跟随的初始化时机，兼容游标续读及其他模式。
- 验证：TDD、全量竞态、vet/build、npm、lint 与复审通过；真实任务 505 确认修复版完整展示原版遗漏的部署失败内容。

# AI 变更记录
