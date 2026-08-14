<h1 align="center">试卷二维码打包格式规范</h1>

> 版本：v1（2026-08-02 定稿）
> 约束双方：印刷系统（生成）与扫码/分类端（解析）。

## 一. 概述

&emsp;&emsp;本格式把试卷标识的全部字段压进一个 56 bit 整数，QR 用 numeric 模式编码，
**任意纠错等级下 v1（21×21）即可装下**。

## 二. 载荷内容与位布局

### 1. 载荷
&emsp;&emsp;QR 码内容 **固定 17 位十进制数字**（无分隔符、无后缀，左补零）。
如：00284518902665574
### 2. 位布局
&emsp;&emsp;其二进制按 **MSB → LSB**（读取顺序即字段顺序）布局，红框内为五元组（43 bit，bit 5-47）：

<div style="font-family: Consolas, monospace; font-size: 14px; padding: 6px 0; text-align: center;">
<span style="background: #fed7d7; padding: 1px 3px;">位置 1bits</span><span style="background: #bee3f8; padding: 1px 3px;">份数 7bits</span><span style="display: inline-block; border: 2px solid #e53e3e; padding: 0;"><span style="background: #c6f6d5; padding: 1px 3px;">学校 13bits</span><span style="background: #feebc8; padding: 1px 3px;">类型 1bits</span><span style="background: #e9d8fd; padding: 1px 3px;">班号区 19bits</span><span style="background: #e2e8f0; padding: 1px 3px;">试卷编号 10bits</span></span><span style="background: #fbb6ce; padding: 1px 3px;">页码 5bits</span>
</div>

<table cellpadding="6" style="margin: 0 auto; text-align: center; border-collapse: collapse; border: 1px solid #000;">
<tr><th style="border: 1px solid #000;">位段</th><th style="border: 1px solid #000;">字段</th><th style="border: 1px solid #000;">位宽</th><th style="border: 1px solid #000;">范围</th><th style="border: 1px solid #000;">说明</th></tr>
<tr><td style="border: 1px solid #000;">bit 55</td><td style="border: 1px solid #000;"><span style="background: #fed7d7; padding: 1px 3px;">位置</span></td><td style="border: 1px solid #000;">1</td><td style="border: 1px solid #000;">0-1</td><td style="border: 1px solid #000;">QR 在页面上的位置：0=右上，1=左下</td></tr>
<tr><td style="border: 1px solid #000;">bit 48-54</td><td style="border: 1px solid #000;"><span style="background: #bee3f8; padding: 1px 3px;">份数</span></td><td style="border: 1px solid #000;">7</td><td style="border: 1px solid #000;">0-127</td><td style="border: 1px solid #000;">学生份数 = studentid</td></tr>
<tr><td style="border: 1px solid #000;">bit 35-47</td><td style="border: 1px solid #000;"><span style="background: #c6f6d5; padding: 1px 3px;">学校</span></td><td style="border: 1px solid #000;">13</td><td style="border: 1px solid #000;">0-8191</td><td style="border: 1px solid #000;"></td></tr>
<tr><td style="border: 1px solid #000;">bit 34</td><td style="border: 1px solid #000;"><span style="background: #feebc8; padding: 1px 3px;">班级类型</span></td><td style="border: 1px solid #000;">1</td><td style="border: 1px solid #000;">0-1</td><td style="border: 1px solid #000;">0=兴趣班，1=行政班</td></tr>
<tr><td style="border: 1px solid #000;">bit 15-33</td><td style="border: 1px solid #000;"><span style="background: #e9d8fd; padding: 1px 3px;">班号区</span></td><td style="border: 1px solid #000;">19</td><td style="border: 1px solid #000;">—</td><td style="border: 1px solid #000;">按班级类型分支解读（见下）</td></tr>
<tr><td style="border: 1px solid #000;">bit 5-14</td><td style="border: 1px solid #000;"><span style="background: #e2e8f0; padding: 1px 3px;">试卷编号</span></td><td style="border: 1px solid #000;">10</td><td style="border: 1px solid #000;">0-1023</td><td style="border: 1px solid #000;"></td></tr>
<tr><td style="border: 1px solid #000;">bit 0-4</td><td style="border: 1px solid #000;"><span style="background: #fbb6ce; padding: 1px 3px;">页码</span></td><td style="border: 1px solid #000;">5</td><td style="border: 1px solid #000;">0-31</td><td style="border: 1px solid #000;">物理页码（非扫描序号）</td></tr>
<tr><td style="border: 1px solid #000;">bit 5-47</td><td style="border: 1px solid #000;"><span style="border: 2px solid #e53e3e; padding: 1px 3px;">五元组</span></td><td style="border: 1px solid #000;">43</td><td style="border: 1px solid #000;">—</td><td style="border: 1px solid #000;">学校+班级类型+班号区+试卷编号（规则见 二.4）</td></tr>
<tr><td style="border: 1px solid #000;"><b>合计</b></td><td style="border: 1px solid #000;"></td><td style="border: 1px solid #000;"><b>56</b></td><td style="border: 1px solid #000;"></td><td style="border: 1px solid #000;"></td></tr>
</table>

&emsp;&emsp;**班号区（bit 15-33，共 19 bit）按班级类型分支解读**：

&emsp;&emsp;行政班（bit34=1）：

<table cellpadding="6" style="margin: 0 auto; text-align: center; border-collapse: collapse; border: 1px solid #000;">
<tr><th style="border: 1px solid #000;">位段</th><th style="border: 1px solid #000;">字段</th><th style="border: 1px solid #000;">位宽</th><th style="border: 1px solid #000;">范围</th><th style="border: 1px solid #000;">说明</th></tr>
<tr><td style="border: 1px solid #000;">bit 27-33</td><td style="border: 1px solid #000;">级数</td><td style="border: 1px solid #000;">7</td><td style="border: 1px solid #000;">0-127</td><td style="border: 1px solid #000;">该级一年级入学年份的后两位（如 2026 级 → 26）</td></tr>
<tr><td style="border: 1px solid #000;">bit 20-26</td><td style="border: 1px solid #000;">班号</td><td style="border: 1px solid #000;">7</td><td style="border: 1px solid #000;">0-127</td><td style="border: 1px solid #000;"></td></tr>
<tr><td style="border: 1px solid #000;">bit 15-19</td><td style="border: 1px solid #000;">科目</td><td style="border: 1px solid #000;">5</td><td style="border: 1px solid #000;">0-31（码表见 二.3）</td><td style="border: 1px solid #000;"></td></tr>
</table>

&emsp;&emsp;兴趣班（bit34=0）：

<table cellpadding="6" style="margin: 0 auto; text-align: center; border-collapse: collapse; border: 1px solid #000;">
<tr><th style="border: 1px solid #000;">位段</th><th style="border: 1px solid #000;">字段</th><th style="border: 1px solid #000;">位宽</th><th style="border: 1px solid #000;">范围</th><th style="border: 1px solid #000;">说明</th></tr>
<tr><td style="border: 1px solid #000;">bit 26-33</td><td style="border: 1px solid #000;">班号</td><td style="border: 1px solid #000;">8</td><td style="border: 1px solid #000;">0-255</td><td style="border: 1px solid #000;"></td></tr>
<tr><td style="border: 1px solid #000;">bit 15-25</td><td style="border: 1px solid #000;">科目</td><td style="border: 1px solid #000;">11</td><td style="border: 1px solid #000;">0-2047</td><td style="border: 1px solid #000;"></td></tr>
</table>


### 3. 码表

#### 班级类型（1 bit）

<table cellpadding="6" style="margin: 0 auto; text-align: center; border-collapse: collapse; border: 1px solid #000;">
<tr><th style="border: 1px solid #000;">值</th><th style="border: 1px solid #000;">含义</th></tr>
<tr><td style="border: 1px solid #000;">0</td><td style="border: 1px solid #000;">兴趣班</td></tr>
<tr><td style="border: 1px solid #000;">1</td><td style="border: 1px solid #000;">行政班</td></tr>
</table>

#### 科目码表（行政班，5 bit）

<table cellpadding="6" style="margin: 0 auto; text-align: center; border-collapse: collapse; border: 1px solid #000;">
<tr><th style="border: 1px solid #000;">值</th><th style="border: 1px solid #000;">科目</th><th style="border: 1px solid #000;">值</th><th style="border: 1px solid #000;">科目</th></tr>
<tr><td style="border: 1px solid #000;">0</td><td style="border: 1px solid #000;">语文</td><td style="border: 1px solid #000;">10</td><td style="border: 1px solid #000;">信息科技</td></tr>
<tr><td style="border: 1px solid #000;">1</td><td style="border: 1px solid #000;">数学</td><td style="border: 1px solid #000;">11</td><td style="border: 1px solid #000;">通用技术</td></tr>
<tr><td style="border: 1px solid #000;">2</td><td style="border: 1px solid #000;">英语</td><td style="border: 1px solid #000;">12</td><td style="border: 1px solid #000;">体育与健康</td></tr>
<tr><td style="border: 1px solid #000;">3</td><td style="border: 1px solid #000;">物理</td><td style="border: 1px solid #000;">13</td><td style="border: 1px solid #000;">音乐</td></tr>
<tr><td style="border: 1px solid #000;">4</td><td style="border: 1px solid #000;">化学</td><td style="border: 1px solid #000;">14</td><td style="border: 1px solid #000;">美术</td></tr>
<tr><td style="border: 1px solid #000;">5</td><td style="border: 1px solid #000;">生物</td><td style="border: 1px solid #000;">15</td><td style="border: 1px solid #000;">心理健康</td></tr>
<tr><td style="border: 1px solid #000;">6</td><td style="border: 1px solid #000;">政治（道德与法治）</td><td style="border: 1px solid #000;">16</td><td style="border: 1px solid #000;">劳动</td></tr>
<tr><td style="border: 1px solid #000;">7</td><td style="border: 1px solid #000;">历史</td><td style="border: 1px solid #000;">17</td><td style="border: 1px solid #000;">综合实践</td></tr>
<tr><td style="border: 1px solid #000;">8</td><td style="border: 1px solid #000;">地理</td><td style="border: 1px solid #000;">18</td><td style="border: 1px solid #000;">日语</td></tr>
<tr><td style="border: 1px solid #000;">9</td><td style="border: 1px solid #000;">科学</td><td style="border: 1px solid #000;">19</td><td style="border: 1px solid #000;">俄语</td></tr>
<tr><td style="border: 1px solid #000;"></td><td style="border: 1px solid #000;"></td><td style="border: 1px solid #000;">20-30</td><td style="border: 1px solid #000;">预留</td></tr>
<tr><td style="border: 1px solid #000;"></td><td style="border: 1px solid #000;"></td><td style="border: 1px solid #000;"><b>31</b></td><td style="border: 1px solid #000;"><b>作文</b></td></tr>
</table>


### 4. 五元组规则（unique_id，bit 5-47）

&emsp;&emsp;**"学校+班级类型+(级数)+班号+科目+试卷编号"五元组唯一定位一份试卷**，
共 43 bit（bit 5-47）。

&emsp;&emsp;业务上做主键/查询时按整体 43 bit 使用；需要按维度筛选时按子字段位段提取。
注意五元组包含班级类型位，因此同一学校下行政班与兴趣班的五元组
永不冲突。

## 三. 样例

### 1. 行政班

<p align="center"><img src="../../qr_samples/doc_example.png" alt="样例二维码"></p>

**QR 内容**：00284518902665574

**二进制**：

<div style="font-family: Consolas, monospace; font-size: 13px; padding: 6px 0; letter-spacing: 1px; text-align: center;">
<span style="background: #fed7d7; padding: 2px 3px;">0</span><span style="background: #bee3f8; padding: 2px 3px;">0000001</span><span style="display: inline-block; border: 2px solid #e53e3e; padding: 0;"><span style="background: #c6f6d5; padding: 2px 3px;">0000001011000</span><span style="background: #feebc8; padding: 2px 3px;">1</span><span style="background: #e9d8fd; padding: 2px 3px;">0010111</span><span style="background: #e9d8fd; padding: 2px 3px;">0000010</span><span style="background: #b2f5ea; padding: 2px 3px;">00000</span><span style="background: #e2e8f0; padding: 2px 3px;">0010001011</span></span><span style="background: #fbb6ce; padding: 2px 3px;">00110</span>
</div>

<table cellpadding="6" style="margin: 0 auto; text-align: center; border-collapse: collapse; border: 1px solid #000;">
<tr><th style="border: 1px solid #000;">字段</th><th style="border: 1px solid #000;">位段二进制</th><th style="border: 1px solid #000;">十进制</th><th style="border: 1px solid #000;">含义</th></tr>
<tr><td style="border: 1px solid #000;">位置</td><td style="border: 1px solid #000;"><span style="background: #fed7d7; padding: 1px 3px;"><code>0</code></span></td><td style="border: 1px solid #000;">0</td><td style="border: 1px solid #000;">右上</td></tr>
<tr><td style="border: 1px solid #000;">份数</td><td style="border: 1px solid #000;"><span style="background: #bee3f8; padding: 1px 3px;"><code>0000001</code></span></td><td style="border: 1px solid #000;">1</td><td style="border: 1px solid #000;">第 1 份（学生 1 号）</td></tr>
<tr><td style="border: 1px solid #000;">学校</td><td style="border: 1px solid #000;"><span style="background: #c6f6d5; padding: 1px 3px;"><code>0000001011000</code></span></td><td style="border: 1px solid #000;">88</td><td style="border: 1px solid #000;"></td></tr>
<tr><td style="border: 1px solid #000;">班级类型</td><td style="border: 1px solid #000;"><span style="background: #feebc8; padding: 1px 3px;"><code>1</code></span></td><td style="border: 1px solid #000;">1</td><td style="border: 1px solid #000;">行政班</td></tr>
<tr><td style="border: 1px solid #000;">级数</td><td style="border: 1px solid #000;"><span style="background: #e9d8fd; padding: 1px 3px;"><code>0010111</code></span></td><td style="border: 1px solid #000;">23</td><td style="border: 1px solid #000;">一年级入学于 2023 年（取后两位 23）</td></tr>
<tr><td style="border: 1px solid #000;">班号</td><td style="border: 1px solid #000;"><span style="background: #e9d8fd; padding: 1px 3px;"><code>0000010</code></span></td><td style="border: 1px solid #000;">2</td><td style="border: 1px solid #000;">2 班</td></tr>
<tr><td style="border: 1px solid #000;">科目</td><td style="border: 1px solid #000;"><span style="background: #b2f5ea; padding: 1px 3px;"><code>00000</code></span></td><td style="border: 1px solid #000;">0</td><td style="border: 1px solid #000;">语文（码表见 二.3）</td></tr>
<tr><td style="border: 1px solid #000;">试卷编号</td><td style="border: 1px solid #000;"><span style="background: #e2e8f0; padding: 1px 3px;"><code>0010001011</code></span></td><td style="border: 1px solid #000;">139</td><td style="border: 1px solid #000;"></td></tr>
<tr><td style="border: 1px solid #000;">页码</td><td style="border: 1px solid #000;"><span style="background: #fbb6ce; padding: 1px 3px;"><code>00110</code></span></td><td style="border: 1px solid #000;">6</td><td style="border: 1px solid #000;">第 6 页</td></tr>
<tr><td style="border: 1px solid #000;"><b>五元组</b></td><td style="border: 1px solid #000;"><span style="border: 2px solid #e53e3e; padding: 1px 3px;"><span style="background: #c6f6d5; padding: 1px 3px;"><code>0000</code></span>…<span style="background: #e2e8f0; padding: 1px 3px;"><code>1011</code></span></span></td><td style="border: 1px solid #000;">95122686091</td><td style="border: 1px solid #000;">五元组拼接（bit 5-47，中间省略）</td></tr>
</table>

### 2. 兴趣班

<p align="center"><img src="../../qr_samples/interest_example.png" alt="兴趣班样例二维码"></p>

**QR 内容**：63897844176979715

**二进制**：

<div style="font-family: Consolas, monospace; font-size: 13px; padding: 6px 0; letter-spacing: 1px; text-align: center;">
<span style="background: #fed7d7; padding: 2px 3px;">1</span><span style="background: #bee3f8; padding: 2px 3px;">1100011</span><span style="display: inline-block; border: 2px solid #e53e3e; padding: 0;"><span style="background: #c6f6d5; padding: 2px 3px;">0000001011000</span><span style="background: #feebc8; padding: 2px 3px;">0</span><span style="background: #e9d8fd; padding: 2px 3px;">00001100</span><span style="background: #b2f5ea; padding: 2px 3px;">00000101010</span><span style="background: #e2e8f0; padding: 2px 3px;">0000111000</span></span><span style="background: #fbb6ce; padding: 2px 3px;">00011</span>
</div>

<table cellpadding="6" style="margin: 0 auto; text-align: center; border-collapse: collapse; border: 1px solid #000;">
<tr><th style="border: 1px solid #000;">字段</th><th style="border: 1px solid #000;">位段二进制</th><th style="border: 1px solid #000;">十进制</th><th style="border: 1px solid #000;">含义</th></tr>
<tr><td style="border: 1px solid #000;">位置</td><td style="border: 1px solid #000;"><span style="background: #fed7d7; padding: 1px 3px;"><code>1</code></span></td><td style="border: 1px solid #000;">1</td><td style="border: 1px solid #000;">左下</td></tr>
<tr><td style="border: 1px solid #000;">份数</td><td style="border: 1px solid #000;"><span style="background: #bee3f8; padding: 1px 3px;"><code>1100011</code></span></td><td style="border: 1px solid #000;">99</td><td style="border: 1px solid #000;">第 99 份</td></tr>
<tr><td style="border: 1px solid #000;">学校</td><td style="border: 1px solid #000;"><span style="background: #c6f6d5; padding: 1px 3px;"><code>0000001011000</code></span></td><td style="border: 1px solid #000;">88</td><td style="border: 1px solid #000;"></td></tr>
<tr><td style="border: 1px solid #000;">班级类型</td><td style="border: 1px solid #000;"><span style="background: #feebc8; padding: 1px 3px;"><code>0</code></span></td><td style="border: 1px solid #000;">0</td><td style="border: 1px solid #000;">兴趣班</td></tr>
<tr><td style="border: 1px solid #000;">班号</td><td style="border: 1px solid #000;"><span style="background: #e9d8fd; padding: 1px 3px;"><code>00001100</code></span></td><td style="border: 1px solid #000;">12</td><td style="border: 1px solid #000;">兴趣班 12 号（bit 26-33，全 8 位为班号）</td></tr>
<tr><td style="border: 1px solid #000;">科目</td><td style="border: 1px solid #000;"><span style="background: #b2f5ea; padding: 1px 3px;"><code>00000101010</code></span></td><td style="border: 1px solid #000;">42</td><td style="border: 1px solid #000;">兴趣班科目码（bit 15-25）</td></tr>
<tr><td style="border: 1px solid #000;">试卷编号</td><td style="border: 1px solid #000;"><span style="background: #e2e8f0; padding: 1px 3px;"><code>0000111000</code></span></td><td style="border: 1px solid #000;">56</td><td style="border: 1px solid #000;"></td></tr>
<tr><td style="border: 1px solid #000;">页码</td><td style="border: 1px solid #000;"><span style="background: #fbb6ce; padding: 1px 3px;"><code>00011</code></span></td><td style="border: 1px solid #000;">3</td><td style="border: 1px solid #000;">第 3 页</td></tr>
<tr><td style="border: 1px solid #000;"><b>五元组</b></td><td style="border: 1px solid #000;"><span style="border: 2px solid #e53e3e; padding: 1px 3px;"><span style="background: #c6f6d5; padding: 1px 3px;"><code>0000</code></span>…<span style="background: #e2e8f0; padding: 1px 3px;"><code>1000</code></span></span></td><td style="border: 1px solid #000;">94514489400</td><td style="border: 1px solid #000;">五元组拼接（bit 5-47，中间省略）</td></tr>
</table>

## 四. 版本协商

&emsp;&emsp;**本版本的判别特征：QR 内容为 17 位纯数字**（v1 维数在 numeric 模式下的
极限长度，当前 56 bit 字段布局恰好用满）。
由于容量已满，任何新增字段都必然导致位数膨胀、进入更高维数——届时新版本自然以新的位数特征与本版本区分。
是否在后续版本中引入显式版本号字段，留待扩展时酌情决定；本版本不内嵌版本号，解析端按"QR 内容的位数形态"分派解析器即可。

## 五. 解码样例

> 注意：56 bit 超出 32 位整数范围——JS 必须用 `BigInt`，Java 用 `long`。

### 1. 只取五元组（unique_id，43 bit，bit 5-47）

&emsp;&emsp;业务上最常用的操作：不解析全部字段，只提取 43 bit 的五元组
（bit 5-47 = 学校+班级类型+级数+班号+科目+试卷编号）。

```javascript
// JavaScript
function uniqueIdOf(s) {
  if (!/^\d{17}$/.test(s)) throw new Error("not a packed qr");
  return Number((BigInt(s) >> 5n) & ((1n << 43n) - 1n));
}
uniqueIdOf("00284518902665574");  // 95122686091
```

```java
// Java
static long uniqueIdOf(String s) {
    return (Long.parseLong(s) >>> 5) & ((1L << 43) - 1);
}
uniqueIdOf("00284518902665574");  // 95122686091
```

```python
# Python
def unique_id_of(s: str) -> int:
    return (int(s) >> 5) & ((1 << 43) - 1)
unique_id_of("00284518902665574")  # 95122686091
```

### 2. 全字段解析

#### JavaScript / TypeScript

```javascript
function parseQR(s) {
  if (!/^\d{17}$/.test(s)) throw new Error("not a packed qr");
  const v = BigInt(s);
  if (v >= 1n << 56n) throw new Error("overflow");

  const position  = Number((v >> 55n) & 1n);
  const copy      = Number((v >> 48n) & 0x7Fn);
  const school    = Number((v >> 35n) & 0x1FFFn);
  const classType = Number((v >> 34n) & 1n);
  const region    = Number((v >> 15n) & 0x7FFFFn);   // 班号区 19 bit
  const paperId   = Number((v >> 5n)  & 0x3FFn);
  const page      = Number(v & 0x1Fn);

  let grade = 0, classNo, subject;
  if (classType === 1) {              // 行政班：级数7 | 班号7 | 科目5
    grade   = (region >> 12) & 0x7F;
    classNo = (region >> 5)  & 0x7F;
    subject =  region        & 0x1F;
  } else {                            // 兴趣班：班号8 | 科目11
    classNo = (region >> 11) & 0xFF;
    subject =  region        & 0x7FF;
  }
  return { position, copy, school, classType, grade, classNo, subject, paperId, page };
}

// 用例
console.log(parseQR("00284518902665574"));
// { position: 0, copy: 1, school: 88, classType: 1,
//   grade: 23, classNo: 2, subject: 0, paperId: 139, page: 6 }

console.log(parseQR("63897844176979715"));
// { position: 1, copy: 99, school: 88, classType: 0,
//   grade: 0, classNo: 12, subject: 42, paperId: 56, page: 3 }
```

#### Java

```java
public record QrFields(int position, int copy, int school, int classType,
                       int grade, int classNo, int subject,
                       int paperId, int page) {

    public static QrFields parse(String s) {
        if (!s.matches("\\d{17}")) throw new IllegalArgumentException("not a packed qr");
        long v = Long.parseLong(s);          // 56 bit < 2^63，无需 unsigned
        if (v >>> 56 != 0) throw new IllegalArgumentException("overflow");

        int position  = (int) (v >>> 55);
        int copy      = (int) (v >>> 48) & 0x7F;
        int school    = (int) (v >>> 35) & 0x1FFF;
        int classType = (int) (v >>> 34) & 1;
        int region    = (int) (v >>> 15) & 0x7FFFF;   // 班号区 19 bit
        int paperId   = (int) (v >>> 5)  & 0x3FF;
        int page      = (int) v & 0x1F;

        int grade = 0, classNo, subject;
        if (classType == 1) {                 // 行政班：级数7 | 班号7 | 科目5
            grade   = (region >>> 12) & 0x7F;
            classNo = (region >>> 5)  & 0x7F;
            subject =  region         & 0x1F;
        } else {                              // 兴趣班：班号8 | 科目11
            classNo = (region >>> 11) & 0xFF;
            subject =  region         & 0x7FF;
        }
        return new QrFields(position, copy, school, classType,
                            grade, classNo, subject, paperId, page);
    }
}

// 用例
QrFields f = QrFields.parse("00284518902665574");
// QrFields[position=0, copy=1, school=88, classType=1,
//          grade=23, classNo=2, subject=0, paperId=139, page=6]

QrFields g = QrFields.parse("63897844176979715");
// QrFields[position=1, copy=99, school=88, classType=0,
//          grade=0, classNo=12, subject=42, paperId=56, page=3]
```
