# 🤖 AI Trading Bot - 基于DeepSeek的智能交易系统

基于Go语言构建的高频加密货币自动交易框架，集成DeepSeek AI决策引擎、实时市场分析和严格风险控制。

## 🚀 核心特性

### 🧠 AI决策引擎
- **DeepSeek集成**: 标准OpenAI格式+智能重试机制，120秒超时确保充分分析
- **多提供商支持**: 支持DeepSeek和通义千问，统一接口灵活切换
- **思维链分析**: 输出完整推理过程，提高决策透明度

### ⚡ 交易执行
- **混合订单策略**: 市价单进出场 + 市价单风控，确保快速执行
- **币安合约支持**: 完整的期货交易接口，支持多空双向操作
- **自动仓位管理**: 智能仓位计算，严格风险控制

### 📊 反思学习系统
- **夏普比率驱动**: 基于绩效数据的自适应优化
- **多维度反思**: 交易频率、持仓时长、信号质量全面分析
- **绩效阈值机制**: 根据夏普比率自动调整交易策略

### 🛡️ 风险控制
- **实时风控**: 每日亏损限制、最大仓位限制、并发持仓限制
- **智能止损**: 市价止损单确保快速离场
- **利润保护**: 市价止盈单锁定计划利润

## 🏗️ 系统架构

```
┌─────────────────────────────────────────────────┐
│                   TraderManager                  │
│   ┌─────────────┐ ┌─────────────┐ ┌─────────────┐ │
│   │  BTC-Alpha  │ │  ETH-Swing  │ │  BNB-Wave   │ │
│   └─────────────┘ └─────────────┘ └─────────────┘ │
└─────────────────────────────────────────────────┘
         │               │               │
         ▼               ▼               ▼
┌─────────────────────────────────────────────────┐
│                MCP (Model Control)              │
│   ┌─────────────┐ ┌─────────────┐               │
│   │  DeepSeek   │ │    Qwen     │               │
│   └─────────────┘ └─────────────┘               │
└─────────────────────────────────────────────────┘
         │               │               │
         ▼               ▼               ▼
┌─────────────────────────────────────────────────┐
│                Exchange Layer                   │
│   ┌─────────────────────────────────────────┐   │
│   │              Binance Futures            │   │
│   └─────────────────────────────────────────┘   │
└─────────────────────────────────────────────────┘
```

## 🚦 绩效阈值机制

| 夏普比率区间 | 状态 | 交易限制 | 反思动作 |
|-------------|------|----------|----------|
| < -0.5 | 🛑 持续亏损 | 停止交易6周期 | 深度反思交易频率和信号质量 |
| -0.5 ~ 0 | ⚠️ 轻微亏损 | 信心度>80，每小时≤1笔 | 严格风控，降低风险 |
| 0 ~ 0.7 | ✅ 稳定盈利 | 正常交易 | 维持策略，小幅优化 |
| > 0.7 | 🚀 优秀表现 | 适度扩大仓位 | 复制成功模式 |

## 📦 快速开始

### 1. 环境准备
```bash
# 克隆项目
git clone <your-repo-url>
cd AI-BOT

# 安装依赖
go mod download
```

### 2. 配置设置
```bash
# 使用样本配置
cp config.sample.json config.json

# 编辑配置文件，添加您的API密钥
vim config.json
```

### 3. API密钥配置
支持两种方式注入密钥：

1. **环境变量**（推荐，避免误提交）：
   ```bash
   export BINANCE_API_KEY="YOUR_BINANCE_API_KEY"
   export BINANCE_API_SECRET="YOUR_BINANCE_SECRET_KEY"
   export DEEPSEEK_API_KEY="YOUR_DEEPSEEK_API_KEY"
   ```

2. **配置文件**（仅限本地环境）：
   ```json
   {
     "exchanges": {
       "binance": {
         "apiKey": "YOUR_BINANCE_API_KEY",
         "apiSecret": "YOUR_BINANCE_SECRET_KEY"
       }
     },
     "deepseek": {
       "enabled": true,
       "apiKey": "YOUR_DEEPSEEK_API_KEY"
     }
   }
   ```

### 4. 运行交易系统
```bash
# 使用 go run 快速启动
go run ./cmd/trader -config config.json

# 模拟模式（不实际下单）
go run ./cmd/trader -config config.json -dry-run

# 构建二进制后运行
go build -o trader ./cmd/trader
./trader -config config.json -dry-run
```

## 🔧 配置详解

### 交易对配置
支持多个交易对并行运行：
```json
"traders": [
  {
    "name": "btc-alpha",
    "exchange": "binance",
    "symbol": "BTCUSDT",
    "interval": "1m",
    "decisionProvider": "deepseek",
    "settings": {
      "leverage": 5,
      "orderQuantity": 0.00001,
      "riskPerTradePercent": 0.5
    }
  }
]
```

### AI提供商配置
```json
"deepseek": {
  "enabled": true,
  "baseUrl": "https://api.deepseek.com",
  "model": "deepseek-chat",
  "temperature": 0.5,
  "topP": 0.9,
  "maxTokens": 2000,
  "apiKey": "YOUR_DEEPSEEK_API_KEY"
}
```

## 🎯 交易策略

### 混合订单策略
- **进场**: 市价单 - 确保快速成交，不错过机会
- **止损**: 市价单 - 触发时立即平仓，严格控制损失
- **止盈**: 市价单 - 达到目标时平仓，锁定利润

### 风险控制规则
- 单笔风险: ≤1% 账户净值
- 每日最大亏损: ≤5% 账户净值
- 最大并发持仓: 3个交易对
- 最小风险回报比: 1:3

## 📈 性能指标

- **决策速度**: 平均响应时间 < 30秒
- **token消耗**: 约3500 tokens/请求（系统500+用户3000）
- **重试机制**: 3次智能重试，指数退避
- **错误处理**: 网络错误自动重试，业务错误立即返回

## 🔍 监控与日志

### 日志文件
```
logs/
├── ai.deepseek.log      # DeepSeek决策日志
├── main.log             # 主程序日志
├── risk.log             # 风控日志
└── trader.*.log         # 各交易对日志
```

### 数据持久化
```
data/
├── decisions.jsonl      # AI决策记录
└── trades.jsonl         # 交易执行记录
```

## 🚨 安全警告

⚠️ **重要安全提示**: 
- 不要将包含真实API密钥的`config.json`上传到GitHub
- 使用`.gitignore`保护敏感配置文件
- 定期轮换API密钥，限制交易权限
- 始终先在模拟模式下测试策略

## 🤝 贡献指南

1. Fork本项目
2. 创建特性分支 (`git checkout -b feature/AmazingFeature`)
3. 提交更改 (`git commit -m 'Add some AmazingFeature'`)
4. 推送到分支 (`git push origin feature/AmazingFeature`)
5. 打开Pull Request

## 📄 许可证

本项目采用MIT许可证 - 查看 [LICENSE](LICENSE) 文件了解详情。

## 🔄 切换AI提供商

### 从DeepSeek切换到通义千问

#### 1. 修改配置文件
```json
{
  "deepseek": {
    "enabled": false  // 关闭DeepSeek
  },
  "qwen": {
    "enabled": true,  // 启用通义千问
    "baseUrl": "https://dashscope.aliyuncs.com/compatible-mode/v1",
    "model": "qwen-turbo",
    "temperature": 0.4,
    "topP": 0.8
  }
}
```

#### 2. 更新交易者配置
修改每个交易者的`decisionProvider`字段：
```json
"traders": [
  {
    "name": "btc-alpha",
    "exchange": "binance", 
    "symbol": "BTCUSDT",
    "interval": "1m",
    "decisionProvider": "qwen",  // 改为qwen
    "settings": {
      "leverage": 5,
      "orderQuantity": 0.00001
    }
  }
]
```

#### 3. 设置通义千问API密钥
通义千问需要双密钥认证：
```json
{
  "qwen": {
    "enabled": true,
    "apiKey": "your_qwen_api_key",      // API密钥
    "secretKey": "your_qwen_secret_key"  // 秘密密钥
  }
}
```

### 通义千问特性

#### ✅ 优势
- **阿里云生态**: 与阿里云服务深度集成
- **中文优化**: 对中文理解和生成特别优化
- **稳定可靠**: 阿里云基础设施保障服务稳定性

#### ⚙️ 配置参数
```json
"qwen": {
  "enabled": true,
  "baseUrl": "https://dashscope.aliyuncs.com/compatible-mode/v1",
  "model": "qwen-turbo",           // 模型名称
  "temperature": 0.4,              // 温度参数（较低更稳定）
  "topP": 0.8,                     // 核采样参数
  "apiKey": "your_api_key",        // API密钥
  "secretKey": "your_secret_key"   // 秘密密钥
}
```

#### 🔧 模型选择
支持的通义千问模型：
- `qwen-turbo`: 快速响应，适合实时交易决策
- `qwen-plus`: 平衡性能与速度
- `qwen-max`: 最高性能，复杂分析场景

### 切换注意事项

#### 1. 提示词适配
通义千问与DeepSeek的提示词响应可能略有不同，建议：
- 测试关键决策场景
- 调整温度参数获得更稳定输出
- 验证JSON格式响应解析

#### 2. 性能考虑
- 通义千问响应速度可能略有不同
- 注意API调用频率限制
- 监控token使用情况

#### 3. 回退方案
建议保持DeepSeek配置作为备份：
```json
{
  "deepseek": {
    "enabled": false,          // 默认关闭
    "apiKey": "backup_key"     // 保留备份密钥
  },
  "qwen": {
    "enabled": true            // 主要使用通义千问
  }
}
```

### 故障排除

#### ❌ 常见问题
1. **认证失败**: 检查API密钥和秘密密钥是否正确
2. **模型不可用**: 确认模型名称拼写正确
3. **响应格式错误**: 调整温度参数降低随机性

#### ✅ 验证步骤
```bash
# 测试通义千问连接
curl -X POST https://dashscope.aliyuncs.com/compatible-mode/v1 \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"qwen-turbo","messages":[{"role":"user","content":"test"}]}'
```

### 性能对比

| 特性 | DeepSeek | 通义千问 |
|------|----------|----------|
| 响应速度 | ⚡️ 快速 | ⚡️ 快速 |
| 中文优化 | ✅ 优秀 | ✅ 极佳 |
| 稳定性 | ✅ 高 | ✅ 高 |
| 成本 | 💰 按token | 💰 按调用 |
| 配置复杂度 | 🔧 简单 | 🔧 中等 |

通过以上配置，您可以轻松在DeepSeek和通义千问之间切换，选择最适合您需求的AI提供商。

## 🙏 致谢

- [DeepSeek](https://www.deepseek.com/) - 提供强大的AI决策能力
- [Binance](https://www.binance.com/) - 提供稳定的交易API
- [Go语言](https://golang.org/) - 提供高性能的并发框架
- [@Web3Tinkle](https://x.com/Web3Tinkle) - 感谢技术指导和架构设计支持

---

**提示**: 交易有风险，投资需谨慎。本工具仅提供技术实现，不构成投资建议。
