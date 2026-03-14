# AKShare 常用接口速查

## A股数据

### 日K线（历史行情）
```python
import akshare as ak
df = ak.stock_zh_a_hist(symbol="000001", period="daily", start_date="20240101", end_date="20241231", adjust="qfq")
# period: daily / weekly / monthly
# adjust: qfq(前复权) / hfq(后复权) / ""(不复权)
```

### 实时行情
```python
df = ak.stock_zh_a_spot_em()  # 全部A股实时行情
# 可通过 df[df['代码'] == '000001'] 筛选
```

### 个股信息
```python
df = ak.stock_individual_info_em(symbol="000001")  # 个股基本信息
```

### 板块行情
```python
df = ak.stock_board_industry_name_em()  # 行业板块列表
df = ak.stock_board_industry_hist_em(symbol="小金属", period="daily")  # 板块历史行情
```

## 期货数据

### 国内期货日K线
```python
df = ak.futures_zh_daily_sina(symbol="RB0")  # 主力合约
# 品种代码+0 表示主力合约，如 RB0(螺纹钢), IF0(沪深300), AU0(黄金)
```

### 期货实时行情
```python
df = ak.futures_zh_spot(subscribe_list="RB0,IF0,AU0", market="CF")
# market: CF(中金所), SF(上期所), DF(大商所), ZF(郑商所)
```

### 期货品种列表
```python
df = ak.futures_display_main_sina()  # 主力合约列表
```

## 常见品种代码

### A股
| 代码 | 名称 |
|------|------|
| 000001 | 平安银行 |
| 600519 | 贵州茅台 |
| 300750 | 宁德时代 |
| 601398 | 工商银行 |

### 期货（主力合约用品种+0）
| 代码 | 名称 | 交易所 |
|------|------|--------|
| RB0 | 螺纹钢 | 上期所 |
| I0 | 铁矿石 | 大商所 |
| IF0 | 沪深300指数 | 中金所 |
| IC0 | 中证500指数 | 中金所 |
| AU0 | 黄金 | 上期所 |
| CU0 | 铜 | 上期所 |
| AG0 | 白银 | 上期所 |
| MA0 | 甲醇 | 郑商所 |
| TA0 | PTA | 郑商所 |
| SC0 | 原油 | 上期能源 |
