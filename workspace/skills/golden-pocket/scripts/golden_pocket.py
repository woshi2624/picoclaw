#!/usr/bin/env python3
"""
黄金口袋（Golden Pocket）策略分析器
支持 A股（日线/周线）和国内期货（日线/分时/5分钟/15分钟）
依赖：akshare, pandas
"""

import argparse
import sys
from datetime import datetime, timedelta

try:
    import akshare as ak
    import pandas as pd
except ImportError:
    print("缺少依赖，请先运行：pip3 install akshare pandas")
    sys.exit(1)


# ─────────────────────────────────────────────
# 数据获取
# ─────────────────────────────────────────────

def fetch_stock(symbol: str, timeframe: str, lookback: int) -> pd.DataFrame:
    end = datetime.today()
    start = end - timedelta(days=lookback)
    period = "daily" if timeframe == "daily" else "weekly"
    df = ak.stock_zh_a_hist(
        symbol=symbol,
        period=period,
        start_date=start.strftime("%Y%m%d"),
        end_date=end.strftime("%Y%m%d"),
        adjust="qfq",
    )
    df = df.rename(columns={
        "日期": "date",
        "开盘": "open",
        "收盘": "close",
        "最高": "high",
        "最低": "low",
        "成交量": "volume",
    })
    df["date"] = pd.to_datetime(df["date"])
    df = df.sort_values("date").reset_index(drop=True)
    return df[["date", "open", "high", "low", "close", "volume"]]


def fetch_futures(symbol: str, lookback: int) -> pd.DataFrame:
    df = ak.futures_zh_daily_sina(symbol=symbol)
    df = df.rename(columns={
        "date": "date",
        "open": "open",
        "high": "high",
        "low": "low",
        "close": "close",
        "volume": "volume",
    })
    df["date"] = pd.to_datetime(df["date"])
    df = df.sort_values("date").reset_index(drop=True)
    cutoff = df["date"].iloc[-1] - timedelta(days=lookback)
    df = df[df["date"] >= cutoff].reset_index(drop=True)
    return df[["date", "open", "high", "low", "close", "volume"]]


def fetch_futures_minute(symbol: str, period: str, lookback: int) -> pd.DataFrame:
    """
    获取期货分钟级 K 线数据（新浪 API）。
    period: "1"=分时, "5"=5分钟, "15"=15分钟
    lookback: 取最近 N 根 K 线（API 返回量有限，建议 ≤ 2000）
    """
    df = ak.futures_zh_minute_sina(symbol=symbol, period=period)
    # 新浪返回列：datetime / open / high / low / close / volume / hold
    df = df.rename(columns={"datetime": "date"})
    df["date"] = pd.to_datetime(df["date"])
    df = df.sort_values("date").reset_index(drop=True)
    # 取最近 lookback 根
    if len(df) > lookback:
        df = df.iloc[-lookback:].reset_index(drop=True)
    return df[["date", "open", "high", "low", "close", "volume"]]


# ─────────────────────────────────────────────
# 指标计算
# ─────────────────────────────────────────────

def calc_ema(series: pd.Series, period: int) -> pd.Series:
    return series.ewm(span=period, adjust=False).mean()


def find_swing_pivots(df: pd.DataFrame, window: int):
    """返回所有高点/低点枢轴的 index 列表"""
    highs, lows = [], []
    n = len(df)
    for i in range(window, n - window):
        if df["high"].iloc[i] == df["high"].iloc[i - window:i + window + 1].max():
            highs.append(i)
        if df["low"].iloc[i] == df["low"].iloc[i - window:i + window + 1].min():
            lows.append(i)
    return highs, lows


def identify_swing(df: pd.DataFrame, trend: str, swing_window: int, swing_lookback: int):
    """在最近 swing_lookback 根 K 线内识别波段高低点"""
    sub = df.iloc[-swing_lookback:].reset_index(drop=True)
    highs, lows = find_swing_pivots(sub, swing_window)

    if trend == "bullish":
        # 牛市：最近高点枢轴为 swing_high，其前最近低点枢轴为 swing_low
        if highs:
            sh_idx = highs[-1]
            swing_high = sub["high"].iloc[sh_idx]
            swing_high_date = sub["date"].iloc[sh_idx]
            prior_lows = [i for i in lows if i < sh_idx]
            if prior_lows:
                sl_idx = prior_lows[-1]
            else:
                sl_idx = int(sub["low"].iloc[:sh_idx + 1].idxmin()) if sh_idx > 0 else 0
            swing_low = sub["low"].iloc[sl_idx]
            swing_low_date = sub["date"].iloc[sl_idx]
        else:
            swing_high = sub["high"].max()
            swing_high_date = sub.loc[sub["high"].idxmax(), "date"]
            swing_low = sub["low"].min()
            swing_low_date = sub.loc[sub["low"].idxmin(), "date"]

    else:  # bearish
        # 熊市：最近低点枢轴为 swing_low，其前最近高点枢轴为 swing_high
        if lows:
            sl_idx = lows[-1]
            swing_low = sub["low"].iloc[sl_idx]
            swing_low_date = sub["date"].iloc[sl_idx]
            prior_highs = [i for i in highs if i < sl_idx]
            if prior_highs:
                sh_idx = prior_highs[-1]
            else:
                sh_idx = int(sub["high"].iloc[:sl_idx + 1].idxmax()) if sl_idx > 0 else 0
            swing_high = sub["high"].iloc[sh_idx]
            swing_high_date = sub["date"].iloc[sh_idx]
        else:
            swing_high = sub["high"].max()
            swing_high_date = sub.loc[sub["high"].idxmax(), "date"]
            swing_low = sub["low"].min()
            swing_low_date = sub.loc[sub["low"].idxmin(), "date"]

    return swing_high, swing_high_date, swing_low, swing_low_date


def calc_fibonacci(swing_high: float, swing_low: float, trend: str) -> dict:
    wave = swing_high - swing_low
    if trend == "bullish":
        return {
            "f0":   swing_high,
            "f50":  swing_high - 0.500 * wave,
            "f618": swing_high - 0.618 * wave,
            "f65":  swing_high - 0.650 * wave,
            "f786": swing_high - 0.786 * wave,
            "f100": swing_low,
            "tp":   swing_high,
            "sl":   swing_high - 0.786 * wave,
        }
    else:
        return {
            "f0":   swing_low,
            "f50":  swing_low + 0.500 * wave,
            "f618": swing_low + 0.618 * wave,
            "f65":  swing_low + 0.650 * wave,
            "f786": swing_low + 0.786 * wave,
            "f100": swing_high,
            "tp":   swing_low,
            "sl":   swing_low + 0.786 * wave,
        }


def volume_signal(df: pd.DataFrame) -> tuple[str, float]:
    avg5 = df["volume"].iloc[-5:].mean()
    avg20 = df["volume"].iloc[-20:].mean()
    ratio = avg5 / avg20 if avg20 > 0 else 1.0
    if ratio >= 1.3:
        label = "成交量放大"
    elif ratio < 0.7:
        label = "成交量萎缩"
    else:
        label = "成交量正常"
    return label, ratio


def candle_pattern(row: pd.Series) -> str:
    body = abs(row["close"] - row["open"])
    rng = row["high"] - row["low"]
    if rng == 0:
        return "无明显形态"
    lower = min(row["open"], row["close"]) - row["low"]
    upper = row["high"] - max(row["open"], row["close"])
    if lower / rng >= 0.5 and body / rng < 0.3:
        return "长下影线（看涨信号）"
    if upper / rng >= 0.5 and body / rng < 0.3:
        return "长上影线（看跌信号）"
    if row["close"] > row["open"] and body / rng >= 0.7:
        return "大阳线（看涨）"
    if row["close"] < row["open"] and body / rng >= 0.7:
        return "大阴线（看跌）"
    return "无明显形态"


# ─────────────────────────────────────────────
# 格式化
# ─────────────────────────────────────────────

def fmt_price(p: float) -> str:
    if p >= 10000:
        return f"{p:,.2f}"
    if p >= 100:
        return f"{p:.2f}"
    if p >= 1:
        return f"{p:.4f}"
    return f"{p:.6f}"


# ─────────────────────────────────────────────
# 主分析逻辑
# ─────────────────────────────────────────────

def analyze(args):
    symbol = args.symbol.upper()
    market = args.market
    timeframe = args.timeframe
    lookback = args.lookback
    swing_window = args.swing_window
    swing_lookback = args.swing_lookback

    # 分钟级周期映射
    MINUTE_PERIODS = {"1min": "1", "5min": "5", "15min": "15"}
    is_intraday = timeframe in MINUTE_PERIODS

    # 分钟级默认参数（若用户未显式调整则自动覆盖）
    if is_intraday and not args.swing_window_set:
        swing_window = 3
    if is_intraday and not args.swing_lookback_set:
        swing_lookback = {"1min": 120, "5min": 100, "15min": 80}[timeframe]

    # 1. 获取数据
    print(f"正在获取 {symbol} 行情数据...", flush=True)
    try:
        if market == "stock":
            df = fetch_stock(symbol, timeframe, lookback)
        elif is_intraday:
            df = fetch_futures_minute(symbol, MINUTE_PERIODS[timeframe], lookback)
        else:
            df = fetch_futures(symbol, lookback)
    except Exception as e:
        print(f"数据获取失败：{e}")
        sys.exit(1)

    if len(df) < 30:
        print(f"数据量不足（{len(df)} 根），无法分析。")
        sys.exit(1)

    # 2. 200 EMA
    df["ema200"] = calc_ema(df["close"], 200)
    current_price = df["close"].iloc[-1]
    ema200 = df["ema200"].iloc[-1]
    ema200_5ago = df["ema200"].iloc[-6] if len(df) >= 6 else ema200
    # 分钟级显示到分钟，日线只显示日期
    if is_intraday:
        current_date = df["date"].iloc[-1].strftime("%Y-%m-%d %H:%M")
    else:
        current_date = df["date"].iloc[-1].strftime("%Y-%m-%d")

    diff_pct = abs(current_price - ema200) / ema200 * 100
    slope_pct = (ema200 - ema200_5ago) / ema200_5ago * 100

    if diff_pct < 1.0 and abs(slope_pct) < 0.1:
        trend = "ranging"
    elif current_price > ema200:
        trend = "bullish"
    else:
        trend = "bearish"

    # 3. 枢轴 & 斐波那契
    if trend != "ranging":
        swing_high, sh_date, swing_low, sl_date = identify_swing(
            df, trend, swing_window, swing_lookback
        )
        fibs = calc_fibonacci(swing_high, swing_low, trend)
    else:
        swing_high = swing_low = sh_date = sl_date = None
        fibs = {}

    # 4. 成交量 & K线形态
    vol_label, vol_ratio = volume_signal(df)
    pattern = candle_pattern(df.iloc[-1])

    # 5. 判定
    if trend == "ranging":
        verdict = "⛔ 禁止交易（盘整）"
    else:
        if trend == "bullish":
            in_pocket = fibs["f65"] <= current_price <= fibs["f50"]
            past_pocket = current_price < fibs["f786"]
        else:
            in_pocket = fibs["f50"] <= current_price <= fibs["f65"]
            past_pocket = current_price > fibs["f786"]

        has_confirm = vol_ratio >= 1.3 or "看涨" in pattern or "看跌" in pattern

        if past_pocket:
            verdict = "⛔ 已过黄金口袋（错过入场）"
        elif in_pocket and has_confirm:
            verdict = "✅ 符合入场条件"
        elif in_pocket:
            verdict = "⚠️ 等待确认信号"
        else:
            verdict = "❌ 不符合（未进入黄金口袋）"

    # ─────────────────────────────────────────
    # 输出
    # ─────────────────────────────────────────
    trend_label = {"bullish": "牛市", "bearish": "熊市", "ranging": "盘整"}.get(trend, trend)
    tf_label = {
        "daily": "日线", "weekly": "周线",
        "1min": "分时(1min)", "5min": "5分钟", "15min": "15分钟",
    }.get(timeframe, timeframe)
    date_fmt = "%Y-%m-%d %H:%M" if is_intraday else "%Y-%m-%d"

    print()
    print("=" * 64)
    print(f"  黄金口袋策略分析 | {symbol} ({tf_label})  ({current_date})")
    print("=" * 64)
    print(f"  当前价格 : {fmt_price(current_price)}")
    print(f"  200 EMA  : {fmt_price(ema200)}  (5期斜率: {slope_pct:+.3f}%)")
    print(f"  K线形态  : {pattern}  |  {vol_label}（均量比 {vol_ratio:.2f}x）")

    if trend != "ranging":
        print()
        print(f"  波段高点: {fmt_price(swing_high)} ({sh_date.strftime(date_fmt) if sh_date else 'N/A'})  "
              f"|  波段低点: {fmt_price(swing_low)} ({sl_date.strftime(date_fmt) if sl_date else 'N/A'})")

    print()
    print("─" * 64)
    print(f"【最终判定】：{verdict}")
    print()
    print(f"【趋势状态】：【{trend_label}】"
          + (f" 价格 {fmt_price(current_price)} {'高于' if trend == 'bullish' else '低于'} "
             f"200 EMA {fmt_price(ema200)}，偏离 {diff_pct:.2f}%"
             if trend != "ranging"
             else f" 价格贴近 EMA（偏离 {diff_pct:.2f}%），EMA 斜率 {slope_pct:+.3f}%，禁止进场"))

    if trend != "ranging":
        gp_lo = fmt_price(fibs["f65"] if trend == "bullish" else fibs["f50"])
        gp_hi = fmt_price(fibs["f50"] if trend == "bullish" else fibs["f65"])
        core_lo = fmt_price(fibs["f65"] if trend == "bullish" else fibs["f618"])
        core_hi = fmt_price(fibs["f618"] if trend == "bullish" else fibs["f65"])
        print()
        print("【点位计算】：")
        print(f"  - 黄金口袋区间 (0.5 ~ 0.65)：{gp_lo} 至 {gp_hi}")
        print(f"  - 核心强支撑/阻力 (0.618 ~ 0.65)：{core_lo} 至 {core_hi}")
        print(f"  - 止损位 (0.786)：{fmt_price(fibs['sl'])}")
        print(f"  - 止盈位 (前{'高' if trend == 'bullish' else '低'} 0 水平)：{fmt_price(fibs['tp'])}")
        print()
        pocket_status = "处于" if (
            (trend == "bullish" and fibs["f65"] <= current_price <= fibs["f50"]) or
            (trend == "bearish" and fibs["f50"] <= current_price <= fibs["f65"])
        ) else "未处于"
        print(f"【当前状态】：当前价格 {fmt_price(current_price)} {pocket_status}黄金口袋中。")
        print()

        if "✅" in verdict:
            if trend == "bullish":
                action = (f"可在 {gp_lo}~{gp_hi} 区间分批做多，"
                          f"止损 {fmt_price(fibs['sl'])}，目标 {fmt_price(fibs['tp'])}")
            else:
                action = (f"可在 {gp_lo}~{gp_hi} 区间分批做空，"
                          f"止损 {fmt_price(fibs['sl'])}，目标 {fmt_price(fibs['tp'])}")
        elif "⚠️" in verdict:
            action = f"等待确认信号（成交量放大或反转K线），再于 {gp_lo}~{gp_hi} 建仓"
        elif "⛔ 已过" in verdict:
            action = "价格已突破/跌破 0.786，黄金口袋机会已过，不追入"
        else:
            action = f"价格未进入黄金口袋区间（{gp_lo}~{gp_hi}），耐心等待回调"

        print(f"【严格执行动作】：{action}")

    print()
    print("【风控铁律提醒】：务必采用逐仓模式，杠杆不得超过3x-5x；")
    if trend != "ranging" and fibs:
        print(f"                  若跌破/突破0.786（{fmt_price(fibs['sl'])}）必须无条件止损！")
    print("─" * 64)


# ─────────────────────────────────────────────
# 入口
# ─────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser(description="黄金口袋策略分析器")
    parser.add_argument("--symbol", required=True, help="标的代码，如 600519 或 RB0")
    parser.add_argument("--market", required=True, choices=["stock", "futures"],
                        help="stock（A股）或 futures（国内期货）")
    parser.add_argument("--timeframe", default="daily",
                        choices=["daily", "weekly", "1min", "5min", "15min"],
                        help="K线周期（1min/5min/15min 仅支持 futures）")
    parser.add_argument("--lookback", type=int, default=400,
                        help="日线/周线：回溯天数；分钟线：回溯 K 线根数（建议 ≤ 2000）")
    parser.add_argument("--swing-window", type=int, default=5, dest="swing_window",
                        help="枢轴检测窗口 K线数（分钟线默认自动改为 3）")
    parser.add_argument("--swing-lookback", type=int, default=120, dest="swing_lookback",
                        help="波段识别 K线范围（分钟线默认自动调整）")
    args = parser.parse_args()

    # 记录用户是否显式设置了 swing 参数，用于分钟线自动覆盖逻辑
    args.swing_window_set = "--swing-window" in sys.argv
    args.swing_lookback_set = "--swing-lookback" in sys.argv

    if args.market == "futures" and args.timeframe == "weekly":
        print("期货不支持周线，已自动切换为日线。")
        args.timeframe = "daily"

    if args.market == "stock" and args.timeframe in ("1min", "5min", "15min"):
        print("A股暂不支持分钟线，已自动切换为日线。")
        args.timeframe = "daily"

    analyze(args)


if __name__ == "__main__":
    main()
