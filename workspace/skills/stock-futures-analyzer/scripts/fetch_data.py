#!/usr/bin/env python3
"""
获取 A股/国内期货行情数据，输出 CSV 到 stdout。
数据直接在线获取，不保存本地文件。

用法：
    # 日线/周线
    python3 fetch_data.py --symbol 000001 --market stock --days 120
    python3 fetch_data.py --symbol RB0 --market futures --days 60

    # 分钟线（用 --bars 控制根数）
    python3 fetch_data.py --symbol 000001 --market stock --period 5min --bars 200
    python3 fetch_data.py --symbol RB0 --market futures --period 15min --bars 200
"""

import argparse
import sys
import time
from datetime import datetime, timedelta

import akshare as ak
import pandas as pd


# ============================================================
# Rate-limiting wrapper
# ============================================================

def fetch_with_retry(fetch_fn, *args, retries=3, sleep_after=0.5, retry_sleep=2.0, **kwargs):
    """Wrap every AKShare call: retry on failure + sleep after success to avoid rate limits."""
    for attempt in range(retries):
        try:
            result = fetch_fn(*args, **kwargs)
            time.sleep(sleep_after)
            return result
        except Exception as e:
            if attempt < retries - 1:
                print(f"第 {attempt + 1} 次请求失败，{retry_sleep}s 后重试: {e}", file=sys.stderr)
                time.sleep(retry_sleep)
            else:
                raise


# ============================================================
# Daily / weekly data
# ============================================================

def fetch_stock_data(symbol: str, period: str, days: int) -> pd.DataFrame:
    """获取 A股历史日线/周线数据"""
    end_date = datetime.now().strftime("%Y%m%d")
    start_date = (datetime.now() - timedelta(days=days)).strftime("%Y%m%d")

    period_map = {"daily": "daily", "weekly": "weekly"}
    ak_period = period_map.get(period, "daily")

    try:
        df = fetch_with_retry(
            ak.stock_zh_a_hist,
            symbol=symbol,
            period=ak_period,
            start_date=start_date,
            end_date=end_date,
            adjust="qfq",
        )
    except Exception as e:
        print(f"错误: 获取股票 {symbol} 数据失败 - {e}", file=sys.stderr)
        sys.exit(1)

    if df.empty:
        print(f"错误: 未获取到股票 {symbol} 的数据", file=sys.stderr)
        sys.exit(1)

    df = df.rename(columns={
        "日期": "date",
        "开盘": "open",
        "收盘": "close",
        "最高": "high",
        "最低": "low",
        "成交量": "volume",
        "成交额": "amount",
        "振幅": "amplitude",
        "涨跌幅": "pct_change",
        "涨跌额": "change",
        "换手率": "turnover",
    })
    return df


def fetch_futures_data(symbol: str, period: str, days: int) -> pd.DataFrame:
    """获取国内期货历史日线数据"""
    end_date = datetime.now().strftime("%Y%m%d")
    start_date = (datetime.now() - timedelta(days=days)).strftime("%Y%m%d")

    try:
        df = fetch_with_retry(ak.futures_zh_daily_sina, symbol=symbol)
    except Exception as e:
        print(f"错误: 获取期货 {symbol} 数据失败 - {e}", file=sys.stderr)
        sys.exit(1)

    if df.empty:
        print(f"错误: 未获取到期货 {symbol} 的数据", file=sys.stderr)
        sys.exit(1)

    df = df.rename(columns={
        "日期": "date",
        "开盘价": "open",
        "收盘价": "close",
        "最高价": "high",
        "最低价": "low",
        "成交量": "volume",
        "持仓量": "open_interest",
    })

    df["date"] = pd.to_datetime(df["date"]).dt.strftime("%Y-%m-%d")
    df = df[df["date"] >= pd.to_datetime(start_date).strftime("%Y-%m-%d")]
    df = df[df["date"] <= pd.to_datetime(end_date).strftime("%Y-%m-%d")]

    return df


# ============================================================
# Minute data
# ============================================================

# Supported minute periods → AKShare period string
MINUTE_PERIOD_MAP = {
    "1min": "1",
    "5min": "5",
    "15min": "15",
    "30min": "30",
    "60min": "60",
}


def fetch_stock_minute_data(symbol: str, period: str, bars: int) -> pd.DataFrame:
    """获取 A股分钟 K线数据（ak.stock_zh_a_hist_min_em）"""
    ak_period = MINUTE_PERIOD_MAP[period]
    # Fetch enough history to cover `bars` bars; request 30 calendar days for
    # intraday data (trading sessions are ~240 min/day for 5-min bars).
    end_dt = datetime.now()
    start_dt = end_dt - timedelta(days=30)
    start_str = start_dt.strftime("%Y-%m-%d %H:%M:%S")
    end_str = end_dt.strftime("%Y-%m-%d %H:%M:%S")

    try:
        df = fetch_with_retry(
            ak.stock_zh_a_hist_min_em,
            symbol=symbol,
            period=ak_period,
            start_date=start_str,
            end_date=end_str,
            adjust="",
        )
    except Exception as e:
        print(f"错误: 获取股票 {symbol} {period} 分钟线失败 - {e}", file=sys.stderr)
        sys.exit(1)

    if df.empty:
        print(f"错误: 未获取到股票 {symbol} {period} 分钟线数据", file=sys.stderr)
        sys.exit(1)

    df = df.rename(columns={
        "时间": "date",
        "开盘": "open",
        "收盘": "close",
        "最高": "high",
        "最低": "low",
        "成交量": "volume",
        "成交额": "amount",
        "振幅": "amplitude",
        "涨跌幅": "pct_change",
        "涨跌额": "change",
        "换手率": "turnover",
    })

    # Keep only the most recent `bars` rows
    return df.tail(bars).reset_index(drop=True)


def fetch_futures_minute_data(symbol: str, period: str, bars: int) -> pd.DataFrame:
    """获取国内期货分钟 K线数据（ak.futures_zh_minute_sina）"""
    ak_period = MINUTE_PERIOD_MAP[period]

    try:
        df = fetch_with_retry(ak.futures_zh_minute_sina, symbol=symbol, period=ak_period)
    except Exception as e:
        print(f"错误: 获取期货 {symbol} {period} 分钟线失败 - {e}", file=sys.stderr)
        sys.exit(1)

    if df.empty:
        print(f"错误: 未获取到期货 {symbol} {period} 分钟线数据", file=sys.stderr)
        sys.exit(1)

    # Normalize column names: datetime → date
    df = df.rename(columns={"datetime": "date"})

    return df.tail(bars).reset_index(drop=True)


# ============================================================
# Entry point
# ============================================================

ALL_PERIODS = ["daily", "weekly", "1min", "5min", "15min", "30min", "60min"]
MINUTE_PERIODS = set(MINUTE_PERIOD_MAP.keys())


def main():
    parser = argparse.ArgumentParser(description="获取 A股/国内期货行情数据")
    parser.add_argument("--symbol", required=True, help="代码（A股: 000001, 期货: RB0）")
    parser.add_argument("--market", required=True, choices=["stock", "futures"], help="市场类型")
    parser.add_argument("--period", default="daily", choices=ALL_PERIODS, help="K线周期")
    parser.add_argument("--days", type=int, default=120, help="日/周线回看天数（默认120）")
    parser.add_argument("--bars", type=int, default=200,
                        help="分钟线取最近 N 根 K线（默认200，建议 ≤ 2000）")
    args = parser.parse_args()

    is_minute = args.period in MINUTE_PERIODS

    if is_minute:
        if args.market == "stock":
            df = fetch_stock_minute_data(args.symbol, args.period, args.bars)
        else:
            df = fetch_futures_minute_data(args.symbol, args.period, args.bars)
    else:
        if args.market == "stock":
            df = fetch_stock_data(args.symbol, args.period, args.days)
        else:
            df = fetch_futures_data(args.symbol, args.period, args.days)

    # Ensure required columns come first
    required_cols = ["date", "open", "close", "high", "low", "volume"]
    available_cols = [c for c in required_cols if c in df.columns]
    extra_cols = [c for c in df.columns if c not in required_cols]
    df = df[available_cols + extra_cols]

    bar_info = f"--bars {args.bars}" if is_minute else f"--days {args.days}"
    print(f"# symbol={args.symbol},market={args.market},period={args.period},{bar_info}", file=sys.stderr)
    print(f"# 共获取 {len(df)} 条数据", file=sys.stderr)

    df.to_csv(sys.stdout, index=False)


if __name__ == "__main__":
    main()
