#!/usr/bin/env python3
"""
机构级量价分析 + 交易建议脚本。
从 stdin 读取 CSV 行情数据，用机构常用指标综合评分，给出交易策略。

指标权重体系（合计 100%）：
  OFI/TakerRatio 20% | MFI 15% | CMF 15% | VWAP位置 15%
  OBV趋势 10% | 成交量比率 10% | Z-score 10% | 成交量分布(POC/VA) 5%
  ADX / Hurst 作为置信度修正系数
  持仓量变化 bonus ±0.05（期货专属）

用法：
    python3 fetch_data.py --symbol RB0 --market futures | python3 analyze.py
    python3 fetch_data.py --symbol 000001 --market stock --period 15min --bars 300 \\
        | python3 analyze.py --timeframe 15min
"""

import argparse
import json
import sys
from typing import Optional

import numpy as np
import pandas as pd


VALID_TIMEFRAMES = ["daily", "weekly", "1min", "5min", "15min", "30min", "60min"]
MINUTE_TIMEFRAMES = {"1min", "5min", "15min", "30min", "60min"}


# ============================================================
# 指标计算
# ============================================================

def calc_atr(df: pd.DataFrame, period: int = 14) -> pd.DataFrame:
    """ATR（真实波动幅度均值），用于定价止损止盈"""
    hl = df["high"] - df["low"]
    hc = (df["high"] - df["close"].shift(1)).abs()
    lc = (df["low"] - df["close"].shift(1)).abs()
    tr = pd.concat([hl, hc, lc], axis=1).max(axis=1)
    df["ATR"] = tr.rolling(period).mean()
    return df


def calc_vwap(df: pd.DataFrame, timeframe: str = "daily", period: int = 20) -> pd.DataFrame:
    """
    VWAP（成交量加权均价）+ 2σ 偏差带。
    分钟线按交易日 session 重置；日线用滚动 period 根加权均价。
    """
    typical = (df["high"] + df["low"] + df["close"]) / 3
    tp_vol = typical * df["volume"]

    if timeframe in MINUTE_TIMEFRAMES:
        try:
            df["_dt"] = pd.to_datetime(df["date"])
            df["_date"] = df["_dt"].dt.date
            df["_tp_vol"] = tp_vol.values
            cum_tp = df.groupby("_date")["_tp_vol"].cumsum()
            cum_v = df.groupby("_date")["volume"].cumsum()
            df["VWAP"] = cum_tp / cum_v.replace(0, 1e-10)
            df.drop(columns=["_dt", "_date", "_tp_vol"], inplace=True)
        except Exception:
            df["VWAP"] = tp_vol.rolling(period).sum() / df["volume"].rolling(period).sum()
    else:
        df["VWAP"] = tp_vol.rolling(period).sum() / df["volume"].rolling(period).sum()

    dev = df["close"] - df["VWAP"]
    std = dev.rolling(period).std()
    df["VWAP_upper"] = df["VWAP"] + 2 * std
    df["VWAP_lower"] = df["VWAP"] - 2 * std
    return df


def calc_adx(df: pd.DataFrame, period: int = 14) -> pd.DataFrame:
    """ADX（趋势强度，不含方向）：<20 震荡，25-35 趋势，>35 强趋势"""
    hl = df["high"] - df["low"]
    hc = (df["high"] - df["close"].shift(1)).abs()
    lc = (df["low"] - df["close"].shift(1)).abs()
    tr = pd.concat([hl, hc, lc], axis=1).max(axis=1)

    up = df["high"].diff()
    dn = -df["low"].diff()
    dm_plus = up.where((up > dn) & (up > 0), 0.0)
    dm_minus = dn.where((dn > up) & (dn > 0), 0.0)

    atr_s = tr.ewm(span=period, adjust=False).mean()
    di_plus = 100 * dm_plus.ewm(span=period, adjust=False).mean() / atr_s.replace(0, 1e-10)
    di_minus = 100 * dm_minus.ewm(span=period, adjust=False).mean() / atr_s.replace(0, 1e-10)
    dx = 100 * (di_plus - di_minus).abs() / (di_plus + di_minus).replace(0, 1e-10)

    df["ADX"] = dx.ewm(span=period, adjust=False).mean()
    df["DI_plus"] = di_plus
    df["DI_minus"] = di_minus
    return df


def calc_mfi(df: pd.DataFrame, period: int = 14) -> pd.DataFrame:
    """MFI（Money Flow Index）：含成交量的 RSI，反映资金流量强度"""
    typical = (df["high"] + df["low"] + df["close"]) / 3
    mf = typical * df["volume"]
    pos = mf.where(typical > typical.shift(1), 0.0)
    neg = mf.where(typical <= typical.shift(1), 0.0)
    mfr = pos.rolling(period).sum() / neg.rolling(period).sum().replace(0, 1e-10)
    df["MFI"] = 100 - (100 / (1 + mfr))
    return df


def calc_cmf(df: pd.DataFrame, period: int = 20) -> pd.DataFrame:
    """CMF（Chaikin Money Flow）：[-1, 1]，正值为净流入"""
    hl = (df["high"] - df["low"]).replace(0, 1e-10)
    mfm = ((df["close"] - df["low"]) - (df["high"] - df["close"])) / hl
    mfv = mfm * df["volume"]
    df["CMF"] = mfv.rolling(period).sum() / df["volume"].rolling(period).sum().replace(0, 1e-10)
    return df


def calc_obv(df: pd.DataFrame, fast: int = 5, slow: int = 20) -> pd.DataFrame:
    """OBV（On-Balance Volume）：量价同向/背离；快慢均线交叉生成信号"""
    direction = np.where(
        df["close"] > df["close"].shift(1), 1,
        np.where(df["close"] < df["close"].shift(1), -1, 0),
    )
    df["OBV"] = (direction * df["volume"].values).cumsum()
    df["OBV_fast"] = df["OBV"].rolling(fast).mean()
    df["OBV_slow"] = df["OBV"].rolling(slow).mean()
    return df


def calc_volume_ratio(df: pd.DataFrame, period: int = 20) -> pd.DataFrame:
    """成交量比率：当前量 / N 期均量"""
    df["vol_avg"] = df["volume"].rolling(period).mean()
    df["vol_ratio"] = df["volume"] / df["vol_avg"].replace(0, 1e-10)
    return df


def calc_ofi(df: pd.DataFrame, period: int = 10) -> pd.DataFrame:
    """
    OFI / Taker Ratio（OHLCV 近似）：
    buy_vol  = volume × (close − low)  / (high − low)
    sell_vol = volume × (high − close) / (high − low)
    taker_ratio = Σbuy_vol / Σtotal_vol（rolling period 根）
    """
    hl = (df["high"] - df["low"]).clip(lower=1e-10)
    buy_vol = df["volume"] * (df["close"] - df["low"]) / hl
    sell_vol = df["volume"] * (df["high"] - df["close"]) / hl
    vol_sum = (buy_vol + sell_vol).rolling(period).sum().replace(0, 1e-10)
    df["taker_ratio"] = buy_vol.rolling(period).sum() / vol_sum
    df["OFI"] = (buy_vol - sell_vol).rolling(period).mean()
    return df


def calc_zscore(df: pd.DataFrame, period: int = 20) -> pd.DataFrame:
    """Z-score：价格偏离 N 期均值的标准差倍数，均值回归信号"""
    mean = df["close"].rolling(period).mean()
    std = df["close"].rolling(period).std()
    df["zscore"] = (df["close"] - mean) / std.replace(0, 1e-10)
    return df


def calc_garman_klass_vol(df: pd.DataFrame, period: int = 20) -> pd.DataFrame:
    """Garman-Klass 波动率（年化%）：比收盘价方差更有效的波动率估计"""
    log_hl = np.log((df["high"] / df["low"].replace(0, 1e-10)).clip(lower=1e-10))
    log_co = np.log((df["close"] / df["open"].replace(0, 1e-10)).clip(lower=1e-10))
    gk = 0.5 * log_hl ** 2 - (2 * np.log(2) - 1) * log_co ** 2
    df["GK_vol"] = np.sqrt(gk.rolling(period).mean() * 252) * 100
    return df


def calc_realized_vol(df: pd.DataFrame, period: int = 20) -> pd.DataFrame:
    """已实现波动率（年化%）"""
    log_ret = np.log(df["close"] / df["close"].shift(1).replace(0, 1e-10))
    df["realized_vol"] = log_ret.rolling(period).std() * np.sqrt(252) * 100
    return df


def calc_amihud(df: pd.DataFrame, period: int = 20) -> pd.DataFrame:
    """Amihud 非流动性比率：|收益率| / 成交量，值越大流动性越差"""
    log_ret_abs = np.log(df["close"] / df["close"].shift(1).replace(0, 1e-10)).abs()
    df["amihud"] = (log_ret_abs / df["volume"].replace(0, 1e-10)).rolling(period).mean()
    return df


def calc_hurst(df: pd.DataFrame, period: int = 100) -> pd.DataFrame:
    """
    Hurst 指数（R/S 分析，最近 period 根 K 线计算一次）：
    > 0.55 趋势市 | < 0.45 均值回归 | 0.45-0.55 随机游走
    """
    if len(df) < period + 1:
        df["hurst"] = 0.5
        return df

    returns = df["close"].pct_change().dropna().tail(period).values

    def _rs(series: np.ndarray) -> float:
        n = len(series)
        if n < 8:
            return 0.5
        lags = range(2, min(20, n // 2))
        pairs = []
        for lag in lags:
            rs_list = []
            for start in range(0, n - lag, lag):
                seg = series[start: start + lag]
                s = seg.std()
                if s <= 0:
                    continue
                dev = (seg - seg.mean()).cumsum()
                rs_list.append((dev.max() - dev.min()) / s)
            if rs_list:
                pairs.append((lag, float(np.mean(rs_list))))
        if len(pairs) < 3:
            return 0.5
        log_l = np.log([p[0] for p in pairs])
        log_r = np.log([p[1] for p in pairs])
        valid = np.isfinite(log_l) & np.isfinite(log_r)
        if valid.sum() < 3:
            return 0.5
        return float(np.clip(np.polyfit(log_l[valid], log_r[valid], 1)[0], 0.01, 0.99))

    df["hurst"] = _rs(returns)
    return df


def calc_volume_profile(df: pd.DataFrame, period: int = 20, buckets: int = 50) -> pd.DataFrame:
    """
    近似 Volume Profile（成交量分布）：
    - POC：成交量最密集价格
    - VAH / VAL：承载 70% 成交量的价格区间上下沿
    """
    recent = df.tail(period)
    p_min = float(recent["low"].min())
    p_max = float(recent["high"].max())
    p_range = p_max - p_min

    if p_range < 1e-10:
        p = float(df["close"].iloc[-1])
        df["POC"], df["VAH"], df["VAL"] = p, p, p
        return df

    bucket_sz = p_range / buckets
    vol_dist = np.zeros(buckets)

    for _, row in recent.iterrows():
        lo_b = max(0, min(int((float(row["low"]) - p_min) / bucket_sz), buckets - 1))
        hi_b = max(0, min(int((float(row["high"]) - p_min) / bucket_sz), buckets - 1))
        n_b = hi_b - lo_b + 1
        vol_dist[lo_b: hi_b + 1] += float(row["volume"]) / n_b

    poc_idx = int(np.argmax(vol_dist))

    # Expand from POC outward until 70% of total volume is covered
    target = vol_dist.sum() * 0.70
    lo = hi = poc_idx
    acc = vol_dist[poc_idx]
    while acc < target:
        can_lo, can_hi = lo > 0, hi < buckets - 1
        if not can_lo and not can_hi:
            break
        lo_v = vol_dist[lo - 1] if can_lo else 0.0
        hi_v = vol_dist[hi + 1] if can_hi else 0.0
        if hi_v >= lo_v and can_hi:
            hi += 1; acc += vol_dist[hi]
        elif can_lo:
            lo -= 1; acc += vol_dist[lo]
        else:
            hi += 1; acc += vol_dist[hi]

    df["POC"] = round(p_min + (poc_idx + 0.5) * bucket_sz, 4)
    df["VAH"] = round(p_min + (hi + 1) * bucket_sz, 4)
    df["VAL"] = round(p_min + lo * bucket_sz, 4)
    return df


def calc_open_interest_change(df: pd.DataFrame) -> pd.DataFrame:
    """持仓量变化（期货日线 open_interest / 分钟线 hold）"""
    oi_col = "open_interest" if "open_interest" in df.columns else "hold" if "hold" in df.columns else None
    if oi_col:
        df[oi_col] = pd.to_numeric(df[oi_col], errors="coerce")
        df["oi_change"] = df[oi_col].diff()
        df["oi_change_pct"] = df[oi_col].pct_change() * 100
    return df


def calc_time_effect(df: pd.DataFrame) -> Optional[dict]:
    """分钟线时段效应：统计各小时历史平均涨跌，标注当前时段强弱"""
    try:
        df_t = df.copy()
        df_t["_hour"] = pd.to_datetime(df_t["date"]).dt.hour
    except Exception:
        return None

    df_t["_ret"] = df_t["close"].pct_change() * 100
    hourly = df_t.groupby("_hour")["_ret"].agg(["mean", "count"])
    hourly = hourly[hourly["count"] >= 5]
    if hourly.empty:
        return None

    cur_h = int(df_t.iloc[-1]["_hour"])
    row = hourly[hourly.index == cur_h]
    if row.empty:
        return None

    avg_ret = float(row.iloc[0]["mean"])
    return {
        "当前时段": f"{cur_h:02d}:xx",
        "当前时段历史均收益(%)": round(avg_ret, 4),
        "时段特征": "历史偏强" if avg_ret > 0.05 else "历史偏弱" if avg_ret < -0.05 else "历史中性",
        "历史最强时段": f"{int(hourly['mean'].idxmax()):02d}:xx",
        "历史最弱时段": f"{int(hourly['mean'].idxmin()):02d}:xx",
    }


# ============================================================
# 信号评分
# ============================================================

def evaluate_signals(df: pd.DataFrame) -> dict:
    """
    机构指标综合评分（基础权重合计 100%）：
      OFI 20% | MFI 15% | CMF 15% | VWAP 15%
      OBV 10% | 成交量比率 10% | Z-score 10% | POC/VA 5%
    置信度修正：ADX（趋势强度）、Hurst（市场性质）
    期货专属 bonus：持仓量变化 ±0.05
    """
    latest = df.iloc[-1]
    prev = df.iloc[-2]
    close = float(latest["close"])
    price_up = close > float(prev["close"])

    signals: dict = {}
    score = 0.0

    # --- OFI / Taker Ratio (20%) ---
    if "taker_ratio" in df.columns and pd.notna(latest.get("taker_ratio")):
        taker = float(latest["taker_ratio"])
        ofi_val = float(latest["OFI"]) if pd.notna(latest.get("OFI")) else 0.0
        if taker > 0.58:
            s = 1.0
        elif taker > 0.52:
            s = 0.5
        elif taker < 0.42:
            s = -1.0
        elif taker < 0.48:
            s = -0.5
        else:
            s = 0.0
        signals["OFI"] = {"score": s, "taker_ratio": round(taker, 4), "OFI": round(ofi_val, 2)}
        score += s * 0.20

    # --- MFI (15%) ---
    if "MFI" in df.columns and pd.notna(latest.get("MFI")):
        mfi = float(latest["MFI"])
        if mfi < 20:
            s = 1.0
        elif mfi > 80:
            s = -1.0
        elif mfi < 40:
            s = 0.5
        elif mfi > 60:
            s = -0.5
        else:
            s = 0.0
        signals["MFI"] = {"score": s, "value": round(mfi, 2)}
        score += s * 0.15

    # --- CMF (15%) ---
    if "CMF" in df.columns and pd.notna(latest.get("CMF")):
        cmf = float(latest["CMF"])
        if cmf > 0.15:
            s = 1.0
        elif cmf > 0.05:
            s = 0.5
        elif cmf < -0.15:
            s = -1.0
        elif cmf < -0.05:
            s = -0.5
        else:
            s = 0.0
        signals["CMF"] = {"score": s, "value": round(cmf, 4)}
        score += s * 0.15

    # --- VWAP 位置 (15%) ---
    if "VWAP" in df.columns and pd.notna(latest.get("VWAP")):
        vwap = float(latest["VWAP"])
        vwap_up = float(latest["VWAP_upper"]) if pd.notna(latest.get("VWAP_upper")) else vwap * 1.02
        vwap_dn = float(latest["VWAP_lower"]) if pd.notna(latest.get("VWAP_lower")) else vwap * 0.98
        if close > vwap_up:
            s = -0.5    # 偏离上轨，超买
        elif close > vwap:
            s = 1.0     # 站上 VWAP，偏多
        elif close < vwap_dn:
            s = 0.5     # 触及下轨，可能反弹
        else:
            s = -1.0    # VWAP 下方，偏空
        signals["VWAP"] = {
            "score": s,
            "VWAP": round(vwap, 2),
            "上轨": round(vwap_up, 2),
            "下轨": round(vwap_dn, 2),
            "价格位置": "上方" if close > vwap else "下方",
        }
        score += s * 0.15

    # --- OBV 趋势 (10%) ---
    if "OBV_fast" in df.columns and pd.notna(latest.get("OBV_fast")) and pd.notna(latest.get("OBV_slow")):
        obv_f = float(latest["OBV_fast"])
        obv_s = float(latest["OBV_slow"])
        prev_f = float(prev["OBV_fast"]) if pd.notna(prev.get("OBV_fast")) else obv_f
        prev_s = float(prev["OBV_slow"]) if pd.notna(prev.get("OBV_slow")) else obv_s
        # Golden/dead cross gives stronger signal
        if prev_f <= prev_s and obv_f > obv_s:
            s = 1.5
        elif prev_f >= prev_s and obv_f < obv_s:
            s = -1.5
        elif obv_f > obv_s:
            s = 1.0
        else:
            s = -1.0
        s = float(np.clip(s, -2, 2))
        signals["OBV"] = {
            "score": s,
            "趋势": "上升" if obv_f > obv_s else "下降",
            "OBV_fast_MA": round(obv_f, 0),
            "OBV_slow_MA": round(obv_s, 0),
        }
        score += s * 0.10

    # --- 成交量比率 (10%) ---
    if "vol_ratio" in df.columns and pd.notna(latest.get("vol_ratio")):
        vr = float(latest["vol_ratio"])
        if vr > 1.5:
            s = 1.0 if price_up else -1.0
        elif vr > 1.0:
            s = 0.5 if price_up else -0.5
        else:
            s = -0.3 if price_up else 0.3  # 缩量反向提示
        signals["成交量比率"] = {"score": s, "vol_ratio": round(vr, 2)}
        score += s * 0.10

    # --- Z-score (10%) ---
    if "zscore" in df.columns and pd.notna(latest.get("zscore")):
        z = float(latest["zscore"])
        if z < -2.0:
            s = 1.0
        elif z > 2.0:
            s = -1.0
        elif z < -1.0:
            s = 0.5
        elif z > 1.0:
            s = -0.5
        else:
            s = 0.0
        signals["Z-score"] = {"score": s, "value": round(z, 3)}
        score += s * 0.10

    # --- 成交量分布 POC / Value Area (5%) ---
    if "POC" in df.columns and pd.notna(latest.get("POC")):
        poc = float(latest["POC"])
        vah = float(latest.get("VAH") or poc)
        val = float(latest.get("VAL") or poc)
        band = poc * 0.003  # ±0.3% 视为 POC 附近
        if close > vah:
            s, pos_label = 1.0, "VA上方突破"
        elif close < val:
            s, pos_label = -1.0, "VA下方跌破"
        elif abs(close - poc) <= band:
            s, pos_label = 0.0, "POC附近（多空拉锯）"
        elif close > poc:
            s, pos_label = 0.3, "VA内偏多"
        else:
            s, pos_label = -0.3, "VA内偏空"
        signals["成交量分布"] = {
            "score": s,
            "POC": round(poc, 2),
            "VAH": round(vah, 2),
            "VAL": round(val, 2),
            "价格位置": pos_label,
        }
        score += s * 0.05

    # --- ADX 置信度修正（不影响方向，只影响幅度）---
    adx_info: Optional[dict] = None
    if "ADX" in df.columns and pd.notna(latest.get("ADX")):
        adx = float(latest["ADX"])
        di_p = float(latest.get("DI_plus") or 0)
        di_m = float(latest.get("DI_minus") or 0)
        if adx < 20:
            score *= 0.70
            label = "震荡市（信号可靠性低）"
        elif adx < 25:
            label = "弱趋势"
        elif adx < 35:
            score *= 1.05
            label = "趋势市"
        else:
            score *= 1.15
            label = "强趋势"
        adx_info = {"ADX": round(adx, 2), "DI+": round(di_p, 2), "DI-": round(di_m, 2), "市场类型": label}

    # --- Hurst 置信度修正 ---
    hurst_info: Optional[dict] = None
    if "hurst" in df.columns and pd.notna(latest.get("hurst")):
        h = float(latest["hurst"])
        if h < 0.45:
            score *= 0.80
            nature = "均值回归（动量信号可靠性下降）"
        elif h > 0.60:
            score *= 1.10
            nature = "趋势"
        else:
            nature = "随机游走"
        hurst_info = {"value": round(h, 4), "市场性质": nature}

    # --- 持仓量变化 bonus ±0.05（期货专属）---
    if "oi_change" in df.columns and pd.notna(latest.get("oi_change")):
        oi_c = float(latest["oi_change"])
        oi_pct = float(latest.get("oi_change_pct") or 0)
        if oi_c > 0 and price_up:
            oi_s = 0.05    # 多头开仓，趋势确认
        elif oi_c > 0 and not price_up:
            oi_s = -0.05   # 空头开仓，趋势向下
        elif oi_c < 0 and not price_up:
            oi_s = 0.05    # 多头平仓，抛压减弱
        else:
            oi_s = -0.05   # 空头平仓，上涨动力存疑
        oi_col = "open_interest" if "open_interest" in df.columns else "hold"
        signals["持仓量变化"] = {
            "score": oi_s,
            "持仓量": float(latest[oi_col]) if pd.notna(latest.get(oi_col)) else None,
            "变化量": round(oi_c, 0),
            "变化比例(%)": round(oi_pct, 2),
        }
        score += oi_s

    return {
        "signals": signals,
        "total_score": round(score, 4),
        "adx_info": adx_info,
        "hurst_info": hurst_info,
    }


# ============================================================
# 胜率回测
# ============================================================

def backtest_win_rate(df: pd.DataFrame, direction: str, lookback: int = 60) -> float:
    """过去 lookback 根同向持有 5 根后的历史胜率"""
    if len(df) < lookback + 5:
        lookback = max(len(df) - 5, 10)
    recent = df.tail(lookback)
    wins = total = 0
    for i in range(len(recent) - 5):
        entry = float(recent.iloc[i]["close"])
        exit_ = float(recent.iloc[i + 5]["close"])
        if direction == "做多":
            wins += exit_ > entry
        else:
            wins += exit_ < entry
        total += 1
    return round(wins / max(total, 1) * 100, 1)


# ============================================================
# 交易建议生成
# ============================================================

def generate_advice(df: pd.DataFrame, risk_ratio: float, atr_multiplier: float,
                    timeframe: str = "daily") -> dict:
    """生成完整的机构级交易建议"""
    latest = df.iloc[-1]
    current_price = float(latest["close"])
    atr = float(latest["ATR"])

    result_s = evaluate_signals(df)
    total_score = result_s["total_score"]

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
        direction, confidence, stop_loss, take_profit = "观望", "无", None, None

    win_rate = backtest_win_rate(df, direction) if direction != "观望" else None

    # Market state (informational, not scored)
    market_state: dict = {}
    if result_s["adx_info"]:
        market_state["ADX"] = result_s["adx_info"]
    if result_s["hurst_info"]:
        market_state["Hurst指数"] = result_s["hurst_info"]
    for col, label in [("GK_vol", "GK波动率(年化%)"), ("realized_vol", "已实现波动率(年化%)")]:
        if col in df.columns and pd.notna(latest.get(col)):
            market_state[label] = round(float(latest[col]), 2)
    if "amihud" in df.columns and pd.notna(latest.get("amihud")):
        a = float(latest["amihud"])
        market_state["Amihud非流动性"] = {
            "value": round(a, 8),
            "流动性评级": "低" if a > 1e-5 else "中" if a > 1e-6 else "高",
        }

    time_effect = calc_time_effect(df) if timeframe in MINUTE_TIMEFRAMES else None

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
        "机构信号": result_s["signals"],
        "市场状态": market_state,
        "数据日期": str(latest["date"]),
        "注意": "本分析仅供参考，不构成投资建议。请结合基本面和市场环境综合判断。",
    }
    if time_effect:
        result["时段效应"] = time_effect
    return result


# ============================================================
# 计算所有指标的统一入口
# ============================================================

def compute_all_indicators(df: pd.DataFrame, timeframe: str = "daily") -> pd.DataFrame:
    """依次计算所有指标，任何一个失败均跳过（不中断流程）"""
    steps = [
        ("ATR",             lambda d: calc_atr(d)),
        ("VWAP",            lambda d: calc_vwap(d, timeframe=timeframe)),
        ("ADX",             lambda d: calc_adx(d)),
        ("MFI",             lambda d: calc_mfi(d)),
        ("CMF",             lambda d: calc_cmf(d)),
        ("OBV",             lambda d: calc_obv(d)),
        ("成交量比率",       lambda d: calc_volume_ratio(d)),
        ("OFI",             lambda d: calc_ofi(d)),
        ("Z-score",         lambda d: calc_zscore(d)),
        ("GK波动率",        lambda d: calc_garman_klass_vol(d)),
        ("已实现波动率",    lambda d: calc_realized_vol(d)),
        ("Amihud",          lambda d: calc_amihud(d)),
        ("Hurst",           lambda d: calc_hurst(d)),
        ("成交量分布",      lambda d: calc_volume_profile(d)),
        ("持仓量变化",      lambda d: calc_open_interest_change(d)),
    ]
    for name, fn in steps:
        try:
            df = fn(df)
        except Exception as e:
            print(f"警告: {name} 计算跳过 - {e}", file=sys.stderr)
    return df


# ============================================================
# Entry point
# ============================================================

def main():
    parser = argparse.ArgumentParser(description="机构级量价分析 + 交易建议")
    parser.add_argument("--risk-ratio", type=float, default=2.0, help="盈亏比（默认 2.0）")
    parser.add_argument("--atr-multiplier", type=float, default=1.5, help="ATR 止损倍数（默认 1.5）")
    parser.add_argument("--timeframe", default="daily", choices=VALID_TIMEFRAMES,
                        help="K线时间框架（默认 daily）")
    args = parser.parse_args()

    try:
        df = pd.read_csv(sys.stdin)
    except Exception as e:
        print(f"错误: 无法读取输入数据 - {e}", file=sys.stderr)
        sys.exit(1)

    if df.empty or len(df) < 30:
        print("错误: 数据不足，至少需要 30 条K线", file=sys.stderr)
        sys.exit(1)

    for col in ["open", "close", "high", "low", "volume"]:
        if col in df.columns:
            df[col] = pd.to_numeric(df[col], errors="coerce")
    df = df.dropna(subset=["close", "high", "low", "open", "volume"]).copy()

    df = compute_all_indicators(df, timeframe=args.timeframe)
    df = df.dropna(subset=["ATR"])

    if df.empty or len(df) < 2:
        print("错误: 有效数据不足，请增加 --days / --bars 参数", file=sys.stderr)
        sys.exit(1)

    advice = generate_advice(df, args.risk_ratio, args.atr_multiplier, args.timeframe)
    print(json.dumps(advice, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
