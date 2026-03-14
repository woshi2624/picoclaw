#!/usr/bin/env python3
"""
获取 A股/国内期货行情数据，输出 CSV 到 stdout。
数据直接在线获取，不保存本地文件。

用法：
    python3 fetch_data.py --symbol 000001 --market stock --days 120
    python3 fetch_data.py --symbol RB0 --market futures --days 60
"""

import argparse
import sys
from datetime import datetime, timedelta

import akshare as ak
import pandas as pd


def fetch_stock_data(symbol: str, period: str, days: int) -> pd.DataFrame:
    """获取 A股历史行情数据"""
    end_date = datetime.now().strftime("%Y%m%d")
    start_date = (datetime.now() - timedelta(days=days)).strftime("%Y%m%d")

    period_map = {"daily": "daily", "weekly": "weekly"}
    ak_period = period_map.get(period, "daily")

    try:
        df = ak.stock_zh_a_hist(
            symbol=symbol,
            period=ak_period,
            start_date=start_date,
            end_date=end_date,
            adjust="qfq",  # 前复权
        )
    except Exception as e:
        print(f"错误: 获取股票 {symbol} 数据失败 - {e}", file=sys.stderr)
        sys.exit(1)

    if df.empty:
        print(f"错误: 未获取到股票 {symbol} 的数据", file=sys.stderr)
        sys.exit(1)

    # 统一列名为英文
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
    """获取国内期货历史行情数据"""
    end_date = datetime.now().strftime("%Y%m%d")
    start_date = (datetime.now() - timedelta(days=days)).strftime("%Y%m%d")

    try:
        df = ak.futures_zh_daily_sina(symbol=symbol)
    except Exception as e:
        print(f"错误: 获取期货 {symbol} 数据失败 - {e}", file=sys.stderr)
        sys.exit(1)

    if df.empty:
        print(f"错误: 未获取到期货 {symbol} 的数据", file=sys.stderr)
        sys.exit(1)

    # 统一列名
    df = df.rename(columns={
        "日期": "date",
        "开盘价": "open",
        "收盘价": "close",
        "最高价": "high",
        "最低价": "low",
        "成交量": "volume",
        "持仓量": "open_interest",
    })

    # 确保 date 列为字符串以便过滤
    df["date"] = pd.to_datetime(df["date"]).dt.strftime("%Y-%m-%d")
    df = df[df["date"] >= pd.to_datetime(start_date).strftime("%Y-%m-%d")]
    df = df[df["date"] <= pd.to_datetime(end_date).strftime("%Y-%m-%d")]

    return df


def main():
    parser = argparse.ArgumentParser(description="获取 A股/国内期货行情数据")
    parser.add_argument("--symbol", required=True, help="代码（A股: 000001, 期货: RB0）")
    parser.add_argument("--market", required=True, choices=["stock", "futures"], help="市场类型")
    parser.add_argument("--period", default="daily", choices=["daily", "weekly"], help="K线周期")
    parser.add_argument("--days", type=int, default=120, help="回看天数（默认120）")
    args = parser.parse_args()

    if args.market == "stock":
        df = fetch_stock_data(args.symbol, args.period, args.days)
    else:
        df = fetch_futures_data(args.symbol, args.period, args.days)

    # 确保必要列存在
    required_cols = ["date", "open", "close", "high", "low", "volume"]
    available_cols = [c for c in required_cols if c in df.columns]
    extra_cols = [c for c in df.columns if c not in required_cols]

    df = df[available_cols + extra_cols]

    # 添加元数据行作为注释
    print(f"# symbol={args.symbol},market={args.market},period={args.period}", file=sys.stderr)
    print(f"# 共获取 {len(df)} 条数据", file=sys.stderr)

    # 输出 CSV 到 stdout
    df.to_csv(sys.stdout, index=False)


if __name__ == "__main__":
    main()
