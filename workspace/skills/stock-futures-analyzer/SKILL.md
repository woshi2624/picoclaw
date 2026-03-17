---
name: stock-futures-analyzer
description: 分析A股和国内期货市场数据，给出交易建议。支持实时/历史行情获取、技术指标计算（MA/MACD/RSI/KDJ/BOLL/ATR）、综合信号评分，输出开仓方向（做多/做空）、止盈止损价位、胜率估算和盈亏比。支持日线、周线、分钟线（1/5/15/30/60分钟），以及多周期联合分析。当用户需要分析股票走势、查询期货行情、获取交易建议或计算技术指标时使用。
---

# 股票期货分析器

通过 AKShare 获取 A股/国内期货行情数据，使用多指标综合评分给出交易建议。

## 环境要求

需要 Python 3 和以下库：
```bash
pip3 install akshare pandas
```

## 使用流程

### 1. 获取数据并分析（推荐：一步完成）

数据在线获取后直接在内存中分析，不保存到本地文件。

**A股日线分析**（如平安银行 000001）：
```bash
python3 scripts/fetch_data.py --symbol 000001 --market stock --days 120 | python3 scripts/analyze.py
```

**期货日线分析**（如螺纹钢主力 RB0）：
```bash
python3 scripts/fetch_data.py --symbol RB0 --market futures --days 120 | python3 scripts/analyze.py
```

**A股 5分钟线分析**：
```bash
python3 scripts/fetch_data.py --symbol 000001 --market stock --period 5min --bars 200 | python3 scripts/analyze.py --timeframe 5min
```

**期货 15分钟线分析**：
```bash
python3 scripts/fetch_data.py --symbol RB0 --market futures --period 15min --bars 200 | python3 scripts/analyze.py --timeframe 15min
```

### 2. 多周期联合分析（推荐）

一键获取日线 + 分钟线并给出综合建议：

**A股**（日线 + 15min + 5min）：
```bash
python3 scripts/multi_tf_analyze.py --symbol 000001 --market stock
```

**期货**（日线 + 15min + 5min + 1min）：
```bash
python3 scripts/multi_tf_analyze.py --symbol RB0 --market futures
```

**自定义参数**：
```bash
python3 scripts/multi_tf_analyze.py --symbol RB0 --market futures --bars 300 --days 180
```

### 3. 仅获取数据

```bash
python3 scripts/fetch_data.py --symbol 000001 --market stock --days 60
```

### 4. 自定义分析参数

```bash
python3 scripts/fetch_data.py --symbol 000001 --market stock --days 200 | python3 scripts/analyze.py --risk-ratio 3.0 --atr-multiplier 2.0
```

## 参数说明

### fetch_data.py

| 参数 | 说明 | 示例 |
|------|------|------|
| `--symbol` | 代码（A股6位数字，期货品种+0表示主力） | `000001`, `600519`, `RB0`, `IF0` |
| `--market` | 市场类型 | `stock` 或 `futures` |
| `--period` | K线周期 | `daily`（默认）, `weekly`, `1min`, `5min`, `15min`, `30min`, `60min` |
| `--days` | 日/周线回看天数 | `120`（默认） |
| `--bars` | 分钟线取最近 N 根 K线 | `200`（默认），建议 ≤ 2000 |

### analyze.py

| 参数 | 说明 | 默认值 |
|------|------|--------|
| `--risk-ratio` | 盈亏比 | `2.0` |
| `--atr-multiplier` | ATR 止损倍数 | `1.5` |
| `--timeframe` | 时间框架（影响 MA 周期自适应） | `daily` |

`--timeframe` 可选值：`daily`, `weekly`, `1min`, `5min`, `15min`, `30min`, `60min`

### multi_tf_analyze.py

| 参数 | 说明 | 默认值 |
|------|------|--------|
| `--symbol` | 代码 | 必填 |
| `--market` | `stock` 或 `futures` | 必填 |
| `--days` | 日线回看天数 | `120` |
| `--bars` | 分钟线根数 | `200` |
| `--risk-ratio` | 盈亏比 | `2.0` |
| `--atr-multiplier` | ATR 止损倍数 | `1.5` |

## 限流说明

> AKShare 无明确 QPS 文档，每次 API 调用成功后自动 sleep 0.5s，失败时重试 3 次（间隔 2s）。
> 多周期分析时，每个时间框架间额外 sleep 1.0s，避免触发服务器限流。

## 常见代码速查

- **A股**: `000001`（平安银行）、`600519`（茅台）、`300750`（宁德时代）
- **期货主力**: `RB0`（螺纹钢）、`IF0`（沪深300）、`AU0`（黄金）、`CU0`（铜）、`I0`（铁矿石）

## 技术指标详情

详细的技术指标计算公式和信号判定规则见 [indicators.md](references/indicators.md)。

## AKShare API 参考

更多可用的数据接口见 [api-guide.md](references/api-guide.md)。
