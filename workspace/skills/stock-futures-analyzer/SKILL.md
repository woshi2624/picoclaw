---
name: stock-futures-analyzer
description: 分析A股和国内期货市场数据，给出交易建议。支持实时/历史行情获取、技术指标计算（MA/MACD/RSI/KDJ/BOLL/ATR）、成交量比率、OFI/Taker Ratio、持仓量变化、时段效应分析、新闻情绪面，综合信号评分，输出开仓方向（做多/做空）、止盈止损价位、胜率估算和盈亏比。支持日线、周线、分钟线（1/5/15/30/60分钟），以及多周期联合分析。当用户需要分析股票走势、查询期货行情、获取交易建议或计算技术指标时使用。
---

# 股票期货分析器

通过 AKShare 获取 A股/国内期货行情数据，综合技术面 + 量价面 + 持仓面 + 情绪面给出交易建议。

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

**期货**（日线 + 15min + 5min + 1min + 新闻情绪）：
```bash
python3 scripts/multi_tf_analyze.py --symbol RB0 --market futures
```

**跳过新闻情绪（加速）**：
```bash
python3 scripts/multi_tf_analyze.py --symbol RB0 --market futures --no-news
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
| `--no-news` | 跳过新闻情绪获取（加速） | `false` |

## 限流说明

> AKShare 无明确 QPS 文档，每次 API 调用成功后自动 sleep 0.5s，失败时重试 3 次（间隔 2s）。
> 多周期分析时，每个时间框架间额外 sleep 1.0s，避免触发服务器限流。

## 常见代码速查

- **A股**: `000001`（平安银行）、`600519`（茅台）、`300750`（宁德时代）
- **期货主力**: `RB0`（螺纹钢）、`IF0`（沪深300）、`AU0`（黄金）、`CU0`（铜）、`I0`（铁矿石）

## 技术指标详情

详细的技术指标计算公式和信号判定规则见 [indicators.md](references/indicators.md)。

## 量价面与情绪面指标

### 成交量比率（Volume Ratio）
- 当前成交量 / N期均量（默认 N=20）
- 放量上涨/缩量下跌为多头信号；放量下跌/缩量上涨为空头信号
- 权重：11%

### OFI / Taker Ratio
- 从 OHLCV 近似拆分买方成交量和卖方成交量：
  - `buy_vol = volume × (close − low) / (high − low)`
  - `sell_vol = volume × (high − close) / (high − low)`
- `taker_ratio = rolling_sum(buy_vol) / rolling_sum(total_vol)`（默认 10 根）
- `OFI = rolling_mean(buy_vol − sell_vol)`
- taker_ratio > 0.58 为偏多，< 0.42 为偏空
- 权重：10%

### 持仓量变化（Open Interest）
- 期货日线从 `open_interest` 字段获取，期货分钟线从 `hold` 字段获取
- 持仓增加 + 价格上涨 → 多头开仓（趋势确认）
- 持仓增加 + 价格下跌 → 空头开仓（趋势向下）
- 作为奖励项（±0.05）叠加到总分

### 时段效应（Time Effect）
- 仅对分钟线周期（1/5/15/30/60min）生效
- 统计历史上各小时段的平均涨跌幅，标注当前时段的历史偏强/偏弱/中性特征

### 新闻情绪（News Sentiment）
- 股票：通过 `ak.stock_news_em` 获取个股新闻
- 期货：通过 `ak.futures_news_baidu` 按品种关键词搜索
- 用正/负面关键词词库统计情绪得分：`(正面数 − 负面数) / 总计`
- 输出：偏多 / 偏空 / 中性，附情绪评分和新闻条数

## AKShare API 参考

更多可用的数据接口见 [api-guide.md](references/api-guide.md)。
