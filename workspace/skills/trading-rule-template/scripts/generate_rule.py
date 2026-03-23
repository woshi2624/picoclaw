import argparse
import sys
try:
    import akshare as ak
    import pandas as pd
except ImportError:
    print("Please install akshare and pandas: uv pip install akshare pandas")
    sys.exit(1)

def get_futures_data(symbol):
    """
    Get futures daily data from AKShare
    Using ak.futures_zh_daily_sina (returns recent daily data for Sina futures)
    """
    try:
        # Sina futures daily data
        df = ak.futures_zh_daily_sina(symbol=symbol)
        if df.empty:
            return None
        return df
    except Exception as e:
        print(f"Error fetching data for {symbol}: {e}")
        return None

def linear_regression_slope(values):
    """
    Compute a simple OLS slope for equally spaced samples without numpy.
    """
    n = len(values)
    x_mean = (n - 1) / 2
    y_mean = sum(values) / n
    numerator = sum((idx - x_mean) * (value - y_mean) for idx, value in enumerate(values))
    denominator = sum((idx - x_mean) ** 2 for idx in range(n))
    return numerator / denominator if denominator else 0.0

def analyze_data(df):
    """
    Calculate trading-plan inputs:
    - Current Price (last close)
    - Prior-20-day High/Low (exclude current bar)
    - Direction/state using price, MA20 level and MA20 slope
    - Channel regime using 20-day high/low slope
    - ATR (14-day)
    """
    if len(df) < 40:
        return None

    df['date'] = pd.to_datetime(df['date'])
    df = df.sort_values('date').reset_index(drop=True)

    df['ma20'] = df['close'].rolling(window=20).mean()
    df['ma20_prev'] = df['ma20'].shift(1)
    df['prior_20_high'] = df['high'].shift(1).rolling(window=20).max()
    df['prior_20_low'] = df['low'].shift(1).rolling(window=20).min()

    df['prev_close'] = df['close'].shift(1)
    df['tr1'] = df['high'] - df['low']
    df['tr2'] = abs(df['high'] - df['prev_close'])
    df['tr3'] = abs(df['low'] - df['prev_close'])
    df['tr'] = df[['tr1', 'tr2', 'tr3']].max(axis=1)
    df['atr14'] = df['tr'].rolling(window=14).mean()

    latest = df.iloc[-1]
    required = ['ma20', 'ma20_prev', 'prior_20_high', 'prior_20_low', 'atr14']
    if latest[required].isna().any():
        return None

    current_price = latest['close']
    ma20 = latest['ma20']
    ma20_prev = latest['ma20_prev']
    resistance = latest['prior_20_high']
    support = latest['prior_20_low']
    atr = latest['atr14']
    recent_highs = df['high'].tail(20).tolist()
    recent_lows = df['low'].tail(20).tolist()
    high_slope = linear_regression_slope(recent_highs)
    low_slope = linear_regression_slope(recent_lows)
    close_slope = linear_regression_slope(df['close'].tail(20).tolist())

    entry_price = current_price
    ma_slope_up = ma20 > ma20_prev
    breakout_long = current_price > resistance
    breakout_short = current_price < support
    slope_threshold = max(atr * 0.05, current_price * 0.0015)

    if high_slope > slope_threshold and low_slope > slope_threshold:
        channel_state = "上涨通道"
        channel_detail = (
            f"近20日高点斜率 {high_slope:.2f}、低点斜率 {low_slope:.2f}，"
            f"高低点同步抬升"
        )
    elif high_slope < -slope_threshold and low_slope < -slope_threshold:
        channel_state = "下跌通道"
        channel_detail = (
            f"近20日高点斜率 {high_slope:.2f}、低点斜率 {low_slope:.2f}，"
            f"高低点同步下移"
        )
    else:
        channel_state = "震荡区间"
        channel_detail = (
            f"近20日高点斜率 {high_slope:.2f}、低点斜率 {low_slope:.2f}，"
            f"通道方向不一致或斜率过小"
        )

    if breakout_long:
        breakout_state = "向上突破前20日区间"
    elif breakout_short:
        breakout_state = "向下跌破前20日区间"
    else:
        breakout_state = "未突破前20日区间"

    if breakout_long:
        plan_type = "突破 (做多)"
        direction = "long"
        structure_anchor = resistance
        stop_loss = min(entry_price - 1.5 * atr, structure_anchor - 0.5 * atr)
        target = max(entry_price + 3.0 * atr, entry_price + 2.0 * (entry_price - stop_loss))
        core_logic = (
            f"向上突破前20日压力 {resistance:.2f}，且价格站上 MA20={ma20:.2f}。"
            f" 回踩不破突破位时优先考虑顺势做多。下方参考支撑 {support:.2f}"
        )
    elif breakout_short:
        plan_type = "突破 (做空)"
        direction = "short"
        structure_anchor = support
        stop_loss = max(entry_price + 1.5 * atr, structure_anchor + 0.5 * atr)
        target = min(entry_price - 3.0 * atr, entry_price - 2.0 * (stop_loss - entry_price))
        core_logic = (
            f"向下跌破前20日支撑 {support:.2f}，且价格压在 MA20={ma20:.2f} 下方。"
            f" 反抽不过跌破位时优先考虑顺势做空。上方参考压力 {resistance:.2f}"
        )
    elif current_price > ma20 and ma_slope_up:
        plan_type = "趋势 (做多)"
        direction = "long"
        structure_anchor = max(support, ma20)
        stop_loss = min(entry_price - 1.5 * atr, structure_anchor - 0.5 * atr)
        target = max(entry_price + 3.0 * atr, resistance)
        core_logic = (
            f"价格位于 MA20={ma20:.2f} 上方且 MA20 继续上行，按顺趋势回踩思路处理。"
            f" 前20日支撑 {support:.2f}，前20日压力 {resistance:.2f}，当前处于{channel_state}"
        )
    elif current_price < ma20 and not ma_slope_up:
        plan_type = "趋势 (做空)"
        direction = "short"
        structure_anchor = min(resistance, ma20)
        stop_loss = max(entry_price + 1.5 * atr, structure_anchor + 0.5 * atr)
        target = min(entry_price - 3.0 * atr, support)
        core_logic = (
            f"价格位于 MA20={ma20:.2f} 下方且 MA20 继续下行，按顺趋势反抽思路处理。"
            f" 前20日支撑 {support:.2f}，前20日压力 {resistance:.2f}，当前处于{channel_state}"
        )
    else:
        plan_type = "等待"
        direction = "neutral"
        stop_loss = None
        target = None
        core_logic = (
            f"价格与 MA20={ma20:.2f} 缠绕或均线斜率不清晰，当前更像震荡。"
            f" 先观察前20日区间 [{support:.2f}, {resistance:.2f}] 是否被有效突破，再决定方向"
        )

    return {
        "date": latest['date'].strftime('%Y-%m-%d'),
        "price": current_price,
        "trend_type": plan_type,
        "direction": direction,
        "breakout_state": breakout_state,
        "channel_state": channel_state,
        "channel_detail": channel_detail,
        "core_logic": core_logic,
        "support": f"{support:.2f}",
        "resistance": f"{resistance:.2f}",
        "atr": f"{atr:.2f}",
        "ma20": f"{ma20:.2f}",
        "ma20_slope": f"{(ma20 - ma20_prev):.2f}",
        "close_slope": f"{close_slope:.2f}",
        "high_slope": f"{high_slope:.2f}",
        "low_slope": f"{low_slope:.2f}",
        "entry_price": f"{entry_price:.2f}",
        "stop_loss": f"{stop_loss:.2f}" if stop_loss is not None else "等待突破/回踩后再定",
        "target": f"{target:.2f}" if target is not None else "等待突破/回踩后再定"
    }

def print_template(symbol, analysis):
    if not analysis:
        # Fallback empty template
        analysis = {
            "date": "YYYY-MM-DD",
            "price": "N/A",
            "trend_type": "趋势 / 逆势 / 突破",
            "core_logic": "支撑压力 / 新闻消息 / 联动品种",
            "entry_price": "执行价格",
            "stop_loss": "认赔离场位",
            "target": "预期止盈位"
        }
        print(f"未能获取 {symbol} 的足够数据以生成自动分析，提供空模板。\n")
    
    print(f"# 📈 期货交易标准化规则模板 ({symbol}) - {analysis['date']}\n")
    print("## 一、 给出的交易计划必须包含\n*在给出交易指令之前，必须能清晰回答以下三个问题。*\n")
    print("| 项目 | 结果 |")
    print("| :--- | :--- |")
    print(f"| **1. 交易类型｜趋势 / 逆势 / 突破** | **{analysis['trend_type']}** |")
    print(f"| **2. 突破判断｜是否突破区间** | **{analysis.get('breakout_state', 'N/A')}** |")
    print(f"| **3. 通道判断｜上涨 / 下跌 / 震荡** | **{analysis.get('channel_state', 'N/A')}** |")
    print(f"| **4. 通道细节｜高低点斜率** | **{analysis.get('channel_detail', 'N/A')}** |")
    print(f"| **5. 核心逻辑｜支撑压力 / 新消息 / 联动品种** | **{analysis['core_logic']}** |")
    print("| **6. 确认信号｜K线形态 / 关键指标** | **(需手动确认共振信号)** |")
    print(f"| **7. 入场价｜执行价格** | **{analysis['entry_price']}** |")
    print(f"| **8. 止损价｜认赔离场位** | **{analysis['stop_loss']}** (结构位 +/- 0.5ATR, 且不少于 1.5ATR) |")
    print(f"| **9. 目标价｜预期止盈位** | **{analysis['target']}** |")
    print("| **10. 仓位管理｜投入手数** | **(需结合合约乘数与保证金手动计算)** |")
    print(f"| **11. 关键位｜前20日支撑 / 压力** | **支撑 {analysis.get('support', 'N/A')} / 压力 {analysis.get('resistance', 'N/A')} / ATR {analysis.get('atr', 'N/A')}** |")
    print(f"| **12. 趋势数据｜MA20 / 斜率** | **MA20 {analysis.get('ma20', 'N/A')} / MA20斜率 {analysis.get('ma20_slope', 'N/A')} / Close斜率 {analysis.get('close_slope', 'N/A')} / High斜率 {analysis.get('high_slope', 'N/A')} / Low斜率 {analysis.get('low_slope', 'N/A')}** |")

def main():
    parser = argparse.ArgumentParser(description="Generate Trading Rule Template using AKShare Futures Data")
    parser.add_argument("--symbol", type=str, required=True, help="Futures symbol (e.g., RB0, V0)")
    
    args = parser.parse_args()
    symbol = args.symbol
    
    print(f"🚀 获取 {symbol} 数据中...")
    df = get_futures_data(symbol)
    if df is not None:
        analysis = analyze_data(df)
        print_template(symbol, analysis)
    else:
        print_template(symbol, None)

if __name__ == "__main__":
    main()
