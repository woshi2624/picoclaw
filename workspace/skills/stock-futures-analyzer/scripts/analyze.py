#!/usr/bin/env python3
"""
技术分析 + 交易建议脚本。
从 stdin 读取 CSV 行情数据，计算技术指标并输出交易建议 JSON。

用法：
    python3 fetch_data.py --symbol 000001 --market stock | python3 analyze.py
    python3 fetch_data.py --symbol 000001 --market stock --period 5min --bars 200 | python3 analyze.py --timeframe 5min
    python3 analyze.py --risk-ratio 3.0 --atr-multiplier 2.0 < data.csv
"""

import argparse
import json
import sys

import pandas as pd


# ============================================================
# Timeframe configuration
# ============================================================

# MA periods and the "warmup" column to require after dropna
TIMEFRAME_CONFIG = {
    "daily":  {"ma_periods": [5, 10, 20, 60], "warmup_col": "MA60"},
    "weekly": {"ma_periods": [5, 10, 20, 60], "warmup_col": "MA60"},
    "60min":  {"ma_periods": [10, 20, 60],    "warmup_col": "MA60"},
    "30min":  {"ma_periods": [10, 20, 60],    "warmup_col": "MA60"},
    "15min":  {"ma_periods": [10, 20, 60],    "warmup_col": "MA60"},
    "5min":   {"ma_periods": [5, 20, 60],     "warmup_col": "MA60"},
    "1min":   {"ma_periods": [5, 20],         "warmup_col": "MA20"},
}


# ============================================================
# 技术指标计算
# ============================================================

def calc_ma(df: pd.DataFrame, periods: list) -> pd.DataFrame:
    """计算移动平均线"""
    for p in periods:
        df[f"MA{p}"] = df["close"].rolling(window=p).mean()
    return df


def calc_macd(df: pd.DataFrame, fast: int = 12, slow: int = 26, signal: int = 9) -> pd.DataFrame:
    """计算 MACD（DIF, DEA, MACD柱）"""
    ema_fast = df["close"].ewm(span=fast, adjust=False).mean()
    ema_slow = df["close"].ewm(span=slow, adjust=False).mean()
    df["DIF"] = ema_fast - ema_slow
    df["DEA"] = df["DIF"].ewm(span=signal, adjust=False).mean()
    df["MACD"] = 2 * (df["DIF"] - df["DEA"])
    return df


def calc_rsi(df: pd.DataFrame, period: int = 14) -> pd.DataFrame:
    """计算 RSI"""
    delta = df["close"].diff()
    gain = delta.where(delta > 0, 0).rolling(window=period).mean()
    loss = (-delta.where(delta < 0, 0)).rolling(window=period).mean()
    rs = gain / loss.replace(0, 1e-10)
    df["RSI"] = 100 - (100 / (1 + rs))
    return df


def calc_kdj(df: pd.DataFrame, n: int = 9, m1: int = 3, m2: int = 3) -> pd.DataFrame:
    """计算 KDJ"""
    low_n = df["low"].rolling(window=n).min()
    high_n = df["high"].rolling(window=n).max()
    rsv = (df["close"] - low_n) / (high_n - low_n).replace(0, 1e-10) * 100

    k = rsv.ewm(com=m1 - 1, adjust=False).mean()
    d = k.ewm(com=m2 - 1, adjust=False).mean()
    j = 3 * k - 2 * d

    df["K"] = k
    df["D"] = d
    df["J"] = j
    return df


def calc_boll(df: pd.DataFrame, period: int = 20, std_dev: float = 2.0) -> pd.DataFrame:
    """计算布林带"""
    df["BOLL_MID"] = df["close"].rolling(window=period).mean()
    std = df["close"].rolling(window=period).std()
    df["BOLL_UP"] = df["BOLL_MID"] + std_dev * std
    df["BOLL_DN"] = df["BOLL_MID"] - std_dev * std
    return df


def calc_atr(df: pd.DataFrame, period: int = 14) -> pd.DataFrame:
    """计算 ATR（真实波动幅度均值）"""
    high_low = df["high"] - df["low"]
    high_close = (df["high"] - df["close"].shift(1)).abs()
    low_close = (df["low"] - df["close"].shift(1)).abs()
    tr = pd.concat([high_low, high_close, low_close], axis=1).max(axis=1)
    df["ATR"] = tr.rolling(window=period).mean()
    return df


# ============================================================
# 信号评分
# ============================================================

def evaluate_signals(df: pd.DataFrame, ma_periods: list) -> dict:
    """基于最新数据计算综合交易信号"""
    latest = df.iloc[-1]
    prev = df.iloc[-2]

    signals = {}
    score = 0.0

    # --- MA 均线信号 (权重 25%) ---
    # Use the two most-common MA columns available: smallest and ~20-period
    short_ma = f"MA{ma_periods[0]}"
    # Pick a medium MA (~20): find closest to 20 among available periods
    medium_ma = f"MA{min(ma_periods, key=lambda p: abs(p - 20))}"

    ma_score = 0
    if latest[short_ma] > latest[medium_ma]:
        ma_score += 1
    elif latest[short_ma] < latest[medium_ma]:
        ma_score -= 1

    if prev[short_ma] <= prev[medium_ma] and latest[short_ma] > latest[medium_ma]:
        ma_score += 1
    elif prev[short_ma] >= prev[medium_ma] and latest[short_ma] < latest[medium_ma]:
        ma_score -= 1

    signals["MA"] = {
        "score": ma_score,
        short_ma: round(latest[short_ma], 2),
        medium_ma: round(latest[medium_ma], 2),
    }
    score += ma_score * 0.25

    # --- MACD 信号 (权重 25%) ---
    macd_score = 0
    if latest["DIF"] > latest["DEA"]:
        macd_score += 1
    elif latest["DIF"] < latest["DEA"]:
        macd_score -= 1

    if prev["DIF"] <= prev["DEA"] and latest["DIF"] > latest["DEA"]:
        macd_score += 1
    elif prev["DIF"] >= prev["DEA"] and latest["DIF"] < latest["DEA"]:
        macd_score -= 1

    signals["MACD"] = {"score": macd_score, "DIF": round(latest["DIF"], 4), "DEA": round(latest["DEA"], 4)}
    score += macd_score * 0.25

    # --- RSI 信号 (权重 20%) ---
    rsi_score = 0
    rsi_val = latest["RSI"]
    if rsi_val < 30:
        rsi_score = 1
    elif rsi_val > 70:
        rsi_score = -1
    elif rsi_val < 50:
        rsi_score = 0.5
    else:
        rsi_score = -0.5

    signals["RSI"] = {"score": rsi_score, "value": round(rsi_val, 2)}
    score += rsi_score * 0.20

    # --- KDJ 信号 (权重 15%) ---
    kdj_score = 0
    if prev["K"] <= prev["D"] and latest["K"] > latest["D"]:
        kdj_score = 1
    elif prev["K"] >= prev["D"] and latest["K"] < latest["D"]:
        kdj_score = -1
    elif latest["K"] > latest["D"]:
        kdj_score = 0.5
    else:
        kdj_score = -0.5

    if latest["J"] < 20:
        kdj_score += 0.5
    elif latest["J"] > 80:
        kdj_score -= 0.5

    signals["KDJ"] = {
        "score": kdj_score,
        "K": round(latest["K"], 2),
        "D": round(latest["D"], 2),
        "J": round(latest["J"], 2),
    }
    score += kdj_score * 0.15

    # --- 布林带信号 (权重 15%) ---
    boll_score = 0
    close = latest["close"]
    boll_up = latest["BOLL_UP"]
    boll_dn = latest["BOLL_DN"]
    boll_mid = latest["BOLL_MID"]

    boll_width = boll_up - boll_dn
    if boll_width > 0:
        position = (close - boll_dn) / boll_width
        if position < 0.2:
            boll_score = 1
        elif position > 0.8:
            boll_score = -1
        elif close > boll_mid:
            boll_score = -0.3
        else:
            boll_score = 0.3

    signals["BOLL"] = {
        "score": boll_score,
        "上轨": round(boll_up, 2),
        "中轨": round(boll_mid, 2),
        "下轨": round(boll_dn, 2),
    }
    score += boll_score * 0.15

    return {"signals": signals, "total_score": round(score, 4)}


def backtest_win_rate(df: pd.DataFrame, direction: str, lookback: int = 60) -> float:
    """
    简化的历史回测胜率估算。
    检查过去 lookback 根 K线内，同方向持有 5 根后的盈利概率。
    """
    if len(df) < lookback + 5:
        lookback = max(len(df) - 5, 10)

    recent = df.tail(lookback).copy()
    wins = 0
    total = 0

    for i in range(len(recent) - 5):
        entry_price = recent.iloc[i]["close"]
        exit_price = recent.iloc[i + 5]["close"]

        if direction == "做多":
            if exit_price > entry_price:
                wins += 1
        else:
            if exit_price < entry_price:
                wins += 1
        total += 1

    return round(wins / max(total, 1) * 100, 1)


# ============================================================
# 交易建议生成
# ============================================================

def generate_advice(df: pd.DataFrame, risk_ratio: float, atr_multiplier: float,
                    ma_periods: list) -> dict:
    """生成完整的交易建议"""
    latest = df.iloc[-1]
    current_price = float(latest["close"])
    atr = float(latest["ATR"])

    signal_result = evaluate_signals(df, ma_periods)
    total_score = signal_result["total_score"]

    if total_score > 0.15:
        direction = "做多"
        confidence = "强" if total_score > 0.5 else "中" if total_score > 0.3 else "弱"
        stop_loss = round(current_price - atr * atr_multiplier, 2)
        take_profit = round(current_price + atr * atr_multiplier * risk_ratio, 2)
    elif total_score < -0.15:
        direction = "做空"
        confidence = "强" if total_score < -0.5 else "中" if total_score < -0.3 else "弱"
        stop_loss = round(current_price + atr * atr_multiplier, 2)
        take_profit = round(current_price - atr * atr_multiplier * risk_ratio, 2)
    else:
        direction = "观望"
        confidence = "无"
        stop_loss = None
        take_profit = None

    win_rate = backtest_win_rate(df, direction) if direction != "观望" else None

    result = {
        "交易建议": {
            "方向": direction,
            "信号强度": confidence,
            "综合评分": total_score,
        },
        "价格信息": {
            "当前价格": current_price,
            "入场价": current_price,
            "止损价": stop_loss,
            "止盈价": take_profit,
            "止损距离": round(abs(current_price - stop_loss), 2) if stop_loss else None,
            "止盈距离": round(abs(take_profit - current_price), 2) if take_profit else None,
        },
        "风险评估": {
            "盈亏比": f"1:{risk_ratio}",
            "ATR(14)": round(atr, 2),
            "ATR止损倍数": atr_multiplier,
            "历史胜率(%)": win_rate,
        },
        "技术指标": signal_result["signals"],
        "数据日期": str(latest["date"]),
        "注意": "本分析仅供参考，不构成投资建议。请结合基本面和市场环境综合判断。",
    }

    return result


def main():
    parser = argparse.ArgumentParser(description="技术分析 + 交易建议")
    parser.add_argument("--risk-ratio", type=float, default=2.0, help="盈亏比（默认 2.0）")
    parser.add_argument("--atr-multiplier", type=float, default=1.5, help="ATR 止损倍数（默认 1.5）")
    parser.add_argument(
        "--timeframe",
        default="daily",
        choices=list(TIMEFRAME_CONFIG.keys()),
        help="K线时间框架，影响 MA 周期自适应（默认 daily）",
    )
    args = parser.parse_args()

    tf_cfg = TIMEFRAME_CONFIG[args.timeframe]
    ma_periods = tf_cfg["ma_periods"]
    warmup_col = tf_cfg["warmup_col"]

    try:
        df = pd.read_csv(sys.stdin)
    except Exception as e:
        print(f"错误: 无法读取输入数据 - {e}", file=sys.stderr)
        sys.exit(1)

    if df.empty or len(df) < 30:
        print("错误: 数据不足，至少需要 30 条K线数据", file=sys.stderr)
        sys.exit(1)

    for col in ["open", "close", "high", "low", "volume"]:
        if col in df.columns:
            df[col] = pd.to_numeric(df[col], errors="coerce")

    df = df.dropna(subset=["close", "high", "low"])

    # Compute all indicators
    df = calc_ma(df, ma_periods)
    df = calc_macd(df)
    df = calc_rsi(df)
    df = calc_kdj(df)
    df = calc_boll(df)
    df = calc_atr(df)

    # Drop warmup rows using the slowest MA for this timeframe
    df = df.dropna(subset=[warmup_col, "DEA", "RSI", "K", "BOLL_MID", "ATR"])

    if df.empty or len(df) < 2:
        print(f"错误: 有效数据不足，请增加 --days / --bars 参数（当前 timeframe: {args.timeframe}）",
              file=sys.stderr)
        sys.exit(1)

    advice = generate_advice(df, args.risk_ratio, args.atr_multiplier, ma_periods)
    print(json.dumps(advice, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
