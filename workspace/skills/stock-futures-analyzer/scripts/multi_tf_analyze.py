#!/usr/bin/env python3
"""
多周期联合分析脚本。
一次性获取多个时间框架数据，分别分析后给出综合建议。

用法：
    # A股（日线 + 15min + 5min）
    python3 scripts/multi_tf_analyze.py --symbol 000001 --market stock

    # 期货（日线 + 15min + 5min + 1min）
    python3 scripts/multi_tf_analyze.py --symbol RB0 --market futures

    # 自定义 bars 和参数
    python3 scripts/multi_tf_analyze.py --symbol RB0 --market futures --bars 300 --days 180
"""

import argparse
import json
import sys
import time
from datetime import datetime
from typing import Optional

# Reuse helpers from sibling scripts
sys.path.insert(0, __file__.rsplit("/", 1)[0])  # ensure scripts/ is on path

from fetch_data import (
    fetch_stock_data,
    fetch_futures_data,
    fetch_stock_minute_data,
    fetch_futures_minute_data,
)
from analyze import (
    TIMEFRAME_CONFIG,
    calc_ma,
    calc_macd,
    calc_rsi,
    calc_kdj,
    calc_boll,
    calc_atr,
    evaluate_signals,
    generate_advice,
)

import pandas as pd


# ============================================================
# Per-timeframe analysis (no subprocess — direct function calls)
# ============================================================

def analyze_df(df: pd.DataFrame, timeframe: str,
               risk_ratio: float, atr_multiplier: float) -> Optional[dict]:
    """Run full indicator + advice pipeline on a DataFrame. Returns None on failure."""
    if df is None or df.empty or len(df) < 30:
        return None

    for col in ["open", "close", "high", "low", "volume"]:
        if col in df.columns:
            df[col] = pd.to_numeric(df[col], errors="coerce")

    df = df.dropna(subset=["close", "high", "low"]).copy()

    tf_cfg = TIMEFRAME_CONFIG[timeframe]
    ma_periods = tf_cfg["ma_periods"]
    warmup_col = tf_cfg["warmup_col"]

    df = calc_ma(df, ma_periods)
    df = calc_macd(df)
    df = calc_rsi(df)
    df = calc_kdj(df)
    df = calc_boll(df)
    df = calc_atr(df)

    df = df.dropna(subset=[warmup_col, "DEA", "RSI", "K", "BOLL_MID", "ATR"])

    if df.empty or len(df) < 2:
        return None

    advice = generate_advice(df, risk_ratio, atr_multiplier, ma_periods)
    return advice


# ============================================================
# Comprehensive recommendation logic
# ============================================================

TIMEFRAME_LABELS = {
    "daily":  "日线",
    "15min":  "15分钟",
    "5min":   "5分钟",
    "1min":   "1分钟",
}


def get_综合建议(results: dict) -> str:
    """Derive a plain-Chinese overall recommendation from per-timeframe results."""
    daily = results.get("日线")
    tf15 = results.get("15分钟")

    if daily is None:
        return "数据不足，无法给出综合建议"

    daily_dir = daily["交易建议"]["方向"]

    if daily_dir == "观望":
        return "观望"

    # Check if 15min agrees
    if tf15 is not None and tf15["交易建议"]["方向"] == daily_dir:
        return f"{daily_dir}（日线+15分钟共振）"

    return f"{daily_dir}（日线信号，等待分钟线确认）"


# ============================================================
# Entry point
# ============================================================

def main():
    parser = argparse.ArgumentParser(description="多周期联合技术分析")
    parser.add_argument("--symbol", required=True, help="代码（A股: 000001, 期货: RB0）")
    parser.add_argument("--market", required=True, choices=["stock", "futures"])
    parser.add_argument("--days", type=int, default=120, help="日线回看天数（默认120）")
    parser.add_argument("--bars", type=int, default=200, help="分钟线根数（默认200）")
    parser.add_argument("--risk-ratio", type=float, default=2.0, help="盈亏比（默认2.0）")
    parser.add_argument("--atr-multiplier", type=float, default=1.5, help="ATR止损倍数（默认1.5）")
    args = parser.parse_args()

    is_futures = args.market == "futures"

    # Define the timeframes to fetch: futures also include 1min
    timeframes = ["daily", "15min", "5min"]
    if is_futures:
        timeframes.append("1min")

    tf_results: dict[str, dict | None] = {}
    inter_tf_sleep = 1.0  # extra sleep between timeframe fetches

    for tf in timeframes:
        print(f"正在获取 {args.symbol} {tf} 数据...", file=sys.stderr)
        try:
            if tf == "daily":
                if is_futures:
                    df = fetch_futures_data(args.symbol, "daily", args.days)
                else:
                    df = fetch_stock_data(args.symbol, "daily", args.days)
            else:
                if is_futures:
                    df = fetch_futures_minute_data(args.symbol, tf, args.bars)
                else:
                    df = fetch_stock_minute_data(args.symbol, tf, args.bars)
        except SystemExit:
            df = None
        except Exception as e:
            print(f"警告: 获取 {tf} 数据失败 - {e}", file=sys.stderr)
            df = None

        result = analyze_df(df, tf, args.risk_ratio, args.atr_multiplier) if df is not None else None
        label = TIMEFRAME_LABELS.get(tf, tf)
        tf_results[label] = result

        # Extra inter-timeframe sleep to be kind to AKShare servers
        if tf != timeframes[-1]:
            time.sleep(inter_tf_sleep)

    # Build combined output
    output: dict = {
        "symbol": args.symbol,
        "market": args.market,
        "分析时间": datetime.now().strftime("%Y-%m-%d %H:%M:%S"),
    }

    for label, result in tf_results.items():
        if result is None:
            output[label] = {"错误": "数据不足或获取失败"}
        else:
            output[label] = {
                "方向": result["交易建议"]["方向"],
                "信号强度": result["交易建议"]["信号强度"],
                "综合评分": result["交易建议"]["综合评分"],
                "当前价格": result["价格信息"]["当前价格"],
                "止损价": result["价格信息"]["止损价"],
                "止盈价": result["价格信息"]["止盈价"],
                "ATR(14)": result["风险评估"]["ATR(14)"],
                "历史胜率(%)": result["风险评估"]["历史胜率(%)"],
                "数据日期": result["数据日期"],
            }

    # Convert label-keyed results back for recommendation logic (expects Chinese keys)
    output["综合建议"] = get_综合建议(
        {label: tf_results[label] for label in tf_results}
    )
    output["注意"] = "仅供参考，不构成投资建议"

    print(json.dumps(output, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
