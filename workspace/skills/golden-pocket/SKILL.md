---
name: golden-pocket
description: 斐波那契"黄金口袋（Golden Pocket）"交易策略严格分析。通过 AKShare 自动获取 A股/国内期货 行情数据，计算200 EMA趋势方向（三态：牛市/熊市/盘整禁止进场）、识别最近波段高低点、计算斐波那契关键位（0.5/0.618/0.65/0.786）、检测成交量和K线形态确认信号，判断当前价格是否进入黄金口袋区间，并输出标准化交易执行指令。当用户需要分析 A股或国内期货标的是否符合黄金口袋策略、获取入场位/止损位/止盈位时使用。
---

# 黄金口袋策略分析器

严格执行斐波那契黄金口袋（0.5~0.65 回撤区）策略，通过 AKShare 自动获取 A股/国内期货 数据，无需手动填写行情参数。

## 依赖安装

安装 uv（一次性，之后无需手动管理 Python 或 pip）：

```bash
# macOS / Linux
curl -LsSf https://astral.sh/uv/install.sh | sh

# Windows
powershell -ExecutionPolicy ByPass -c "irm https://astral.sh/uv/install.ps1 | iex"
```

## 快速开始

```bash
# A 股
uv run --with akshare --with pandas scripts/golden_pocket.py --symbol 000001 --market stock
uv run --with akshare --with pandas scripts/golden_pocket.py --symbol 600519 --market stock --timeframe weekly

# 国内期货（主力合约）
uv run --with akshare --with pandas scripts/golden_pocket.py --symbol RB0 --market futures
uv run --with akshare --with pandas scripts/golden_pocket.py --symbol AU0 --market futures

# 期货分钟线
uv run --with akshare --with pandas scripts/golden_pocket.py --symbol RB0 --market futures --timeframe 1min
uv run --with akshare --with pandas scripts/golden_pocket.py --symbol IF0 --market futures --timeframe 5min
uv run --with akshare --with pandas scripts/golden_pocket.py --symbol AU0 --market futures --timeframe 15min
```

## 参数说明

| 参数 | 说明 | 默认值 |
|------|------|--------|
| `--symbol` | 标的代码 | 必填 |
| `--market` | `stock`（A股）/ `futures`（国内期货） | 必填 |
| `--timeframe` | `daily` / `weekly`（仅stock）/ `1min` / `5min` / `15min`（仅futures） | `daily` |
| `--lookback` | 日线/周线：回溯天数（建议 ≥ 400）；分钟线：回溯 K 线根数（建议 ≤ 2000） | `400` |
| `--swing-window` | 枢轴检测窗口 K线数（分钟线自动改为 3） | `5` |
| `--swing-lookback` | 波段识别的 K 线范围（分钟线自动按周期调整） | `120` |

## 常见代码速查

- **A股**：`600519`（茅台）`000001`（平安银行）`300750`（宁德时代）`000858`（五粮液）
- **期货主力**：`RB0`（螺纹钢）`IF0`（沪深300）`AU0`（黄金）`CU0`（铜）`I0`（铁矿石）`AG0`（白银）

## 策略规则

**第一步：趋势过滤（200 EMA）**
- 价格 > EMA → 牛市，只做多
- 价格 < EMA → 熊市，只做空
- 价格贴近EMA且EMA走平 → ⛔ 盘整，禁止进场

**第二步：斐波那契关键位**（基于自动识别的波段高低点）
- 黄金口袋：0.5 ~ 0.65 回撤区
- 核心区：0.618 ~ 0.65（专业反转核心）
- 止损线：0.786（突破/跌破必须无条件止损）
- 止盈位：0%（前高/前低）

**第三步：确认信号**（在黄金口袋内额外加分，否则标注 Fakeout 风险）
- 成交量放大（近5根量 ≥ 近20根均量的1.3倍）
- 反转K线（长下影线 / 长上影线 / 大阳线 / 大阴线）

## 输出格式

```
【最终判定】：✅ 符合 / ⚠️ 等待确认 / ❌ 不符合 / ⛔ 禁止交易
【趋势状态】：200 EMA趋势描述
【点位计算】：0.5 / 0.618 / 0.65 / 0.786 具体价格
【当前状态】：价格是否在黄金口袋中
【严格执行动作】：具体操作指令
【风控铁律提醒】：杠杆和止损纪律
```

## 调参建议

- 若波段识别不准确：调大 `--swing-window`（如 8~10）减少噪音，或缩小 `--swing-lookback` 聚焦近期
- 数据量 < 210 根时 EMA 精度下降，可增大 `--lookback`
- 周线分析时建议 `--lookback 1500 --swing-window 3`
- 分钟线数据量受 API 限制（通常返回最近 1~3 个交易日），`--lookback` 控制使用多少根参与计算
- 1min 波动噪音大，建议搭配 5min/15min 确认方向后再看 1min 精确入场
