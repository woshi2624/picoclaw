#!/usr/bin/env python3
"""
多周期联合分析脚本（机构指标版）。
一次性获取多个时间框架数据，分别计算机构指标并给出综合建议，附新闻情绪面。

用法：
    python3 scripts/multi_tf_analyze.py --symbol 000001 --market stock
    python3 scripts/multi_tf_analyze.py --symbol RB0 --market futures
    python3 scripts/multi_tf_analyze.py --symbol RB0 --market futures --bars 300 --days 180
"""

import argparse
import json
import sys
import time
from datetime import datetime
from typing import Optional

sys.path.insert(0, __file__.rsplit("/", 1)[0])

from fetch_data import (
    fetch_stock_data,
    fetch_futures_data,
    fetch_stock_minute_data,
    fetch_futures_minute_data,
    fetch_with_retry,
)
from analyze import (
    compute_all_indicators,
    generate_advice,
)

import pandas as pd


# ============================================================
# Per-timeframe analysis
# ============================================================

def analyze_df(df: pd.DataFrame, timeframe: str,
               risk_ratio: float, atr_multiplier: float) -> Optional[dict]:
    """Run institutional indicator pipeline on a DataFrame. Returns None on failure."""
    if df is None or df.empty or len(df) < 30:
        return None

    for col in ["open", "close", "high", "low", "volume"]:
        if col in df.columns:
            df[col] = pd.to_numeric(df[col], errors="coerce")
    df = df.dropna(subset=["close", "high", "low", "open", "volume"]).copy()

    df = compute_all_indicators(df, timeframe=timeframe)
    df = df.dropna(subset=["ATR"])

    if df.empty or len(df) < 2:
        return None

    return generate_advice(df, risk_ratio, atr_multiplier, timeframe)


# ============================================================
# 新闻情绪
# ============================================================

_POS = [
    "上涨", "利好", "增长", "强势", "多头", "突破", "拉升", "走强", "反弹", "创新高",
    "超预期", "扩张", "回暖", "改善", "积极", "看多", "牛市", "做多", "买入", "增持",
    "上行", "提振", "好转", "乐观", "涨停", "爆量",
]
_NEG = [
    "下跌", "利空", "下滑", "弱势", "空头", "跌破", "下行", "走弱", "回落", "创新低",
    "不及预期", "收缩", "降温", "恶化", "消极", "看空", "熊市", "做空", "卖出", "减持",
    "拖累", "悲观", "跌停", "缩量", "风险",
]


def fetch_news_sentiment(symbol: str, market: str) -> Optional[dict]:
    """用 AKShare 抓取相关新闻并做简单关键词情绪分析"""
    import akshare as ak
    texts = []
    try:
        if market == "stock":
            df_n = fetch_with_retry(ak.stock_news_em, symbol=symbol)
        else:
            keyword = "".join(c for c in symbol if c.isalpha())
            df_n = fetch_with_retry(ak.futures_news_baidu, keyword=keyword)

        if df_n is not None and not df_n.empty:
            for col in ("新闻标题", "title", "标题"):
                if col in df_n.columns:
                    texts = df_n[col].dropna().tolist()[:30]
                    break
    except Exception as e:
        print(f"警告: 获取新闻失败 - {e}", file=sys.stderr)
        return None

    if not texts:
        return None

    pos = sum(sum(w in t for w in _POS) for t in texts)
    neg = sum(sum(w in t for w in _NEG) for t in texts)
    total = pos + neg
    s = round((pos - neg) / total, 3) if total else 0.0
    return {
        "情绪": "偏多" if s > 0.1 else "偏空" if s < -0.1 else "中性",
        "情绪评分": s,
        "正面信号数": pos,
        "负面信号数": neg,
        "新闻条数": len(texts),
    }


# ============================================================
# 综合建议
# ============================================================

TIMEFRAME_LABELS = {
    "daily": "日线",
    "15min": "15分钟",
    "5min":  "5分钟",
    "1min":  "1分钟",
}


def get_综合建议(results: dict) -> str:
    daily = results.get("日线")
    tf15 = results.get("15分钟")
    if daily is None:
        return "数据不足，无法给出综合建议"
    d = daily["交易建议"]["方向"]
    if d == "观望":
        return "观望"
    if tf15 is not None and tf15["交易建议"]["方向"] == d:
        return f"{d}（日线+15分钟共振）"
    return f"{d}（日线信号，等待分钟线确认）"


# ============================================================
# Entry point
# ============================================================

def main():
    parser = argparse.ArgumentParser(description="多周期机构指标联合分析")
    parser.add_argument("--symbol", required=True)
    parser.add_argument("--market", required=True, choices=["stock", "futures"])
    parser.add_argument("--days", type=int, default=120, help="日线回看天数（默认120）")
    parser.add_argument("--bars", type=int, default=200, help="分钟线根数（默认200）")
    parser.add_argument("--risk-ratio", type=float, default=2.0)
    parser.add_argument("--atr-multiplier", type=float, default=1.5)
    parser.add_argument("--no-news", action="store_true", help="跳过新闻情绪获取")
    args = parser.parse_args()

    is_futures = args.market == "futures"
    timeframes = ["daily", "15min", "5min"] + (["1min"] if is_futures else [])

    tf_results: dict = {}
    for tf in timeframes:
        print(f"正在获取 {args.symbol} {tf} 数据...", file=sys.stderr)
        try:
            if tf == "daily":
                df = fetch_futures_data(args.symbol, "daily", args.days) if is_futures \
                    else fetch_stock_data(args.symbol, "daily", args.days)
            else:
                df = fetch_futures_minute_data(args.symbol, tf, args.bars) if is_futures \
                    else fetch_stock_minute_data(args.symbol, tf, args.bars)
        except SystemExit:
            df = None
        except Exception as e:
            print(f"警告: 获取 {tf} 失败 - {e}", file=sys.stderr)
            df = None

        result = analyze_df(df, tf, args.risk_ratio, args.atr_multiplier) if df is not None else None
        tf_results[TIMEFRAME_LABELS.get(tf, tf)] = result
        if tf != timeframes[-1]:
            time.sleep(1.0)

    news = None
    if not args.no_news:
        print(f"正在获取 {args.symbol} 新闻情绪...", file=sys.stderr)
        news = fetch_news_sentiment(args.symbol, args.market)

    output: dict = {
        "symbol": args.symbol,
        "market": args.market,
        "分析时间": datetime.now().strftime("%Y-%m-%d %H:%M:%S"),
    }

    for label, result in tf_results.items():
        if result is None:
            output[label] = {"错误": "数据不足或获取失败"}
        else:
            tf_out: dict = {
                "方向": result["交易建议"]["方向"],
                "信号强度": result["交易建议"]["信号强度"],
                "综合评分": result["交易建议"]["综合评分"],
                "当前价格": result["价格信息"]["当前价格"],
                "止损价": result["价格信息"]["止损价"],
                "止盈价": result["价格信息"]["止盈价"],
                "盈亏比": result["风险评估"]["盈亏比"],
                "历史胜率(%)": result["风险评估"]["历史胜率(%)"],
                "机构信号": result["机构信号"],
                "市场状态": result["市场状态"],
                "数据日期": result["数据日期"],
            }
            if "时段效应" in result:
                tf_out["时段效应"] = result["时段效应"]
            output[label] = tf_out

    output["综合建议"] = get_综合建议({label: tf_results[label] for label in tf_results})
    output["新闻情绪"] = news if news else "获取失败或无数据"
    output["注意"] = "仅供参考，不构成投资建议"

    print(json.dumps(output, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
