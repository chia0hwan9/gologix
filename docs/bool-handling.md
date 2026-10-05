# BOOL / BOOL 数组处理对比：AB Logix / 汇川 Inovance / Omron NJ·NX

> **本文定位**：实现与排查用的对照参考。回答同一个问题——"这个 BOOL 在线上到底占几个字节、
> 位序如何、gologix 现在怎么做"——按 **顶层标签 / 结构体成员** 两种访问形态列出
> AB Logix、汇川 AT_DEFAULT、汇川 AT_INOPROSHOP 三套规则，并附上 **Omron NJ/NX** 作对照
> （Omron 的规则轴不同，见 §1 表下的警告；其完整差异另见 `omron-nx-nj-eip.md`）。
>
> **依据**：
> - 汇川：深圳市汇川技术《EIP 标签通信库使用说明》V2.0.2.8（2026-01-27）**§4.2**（顶层 Bool）、
>   **§4.4.2**（成员为 Bool）、§4.4.1（成员基础类型对齐）。本地副本
>   `docs/汇川EIP标签通信库使用说明V2.0.2.8.pdf`（不入库，见 `docs/resources.md`）。
>   第 16/17 页的两张排布表是本文 `UDT1`/`UDT2` 字节示例的来源，也是
>   `inovance_pack_test.go` 的夹具。
> - AB Logix：本文描述的是 **gologix 现有实现**（`pack.go` / `read.go` / `write.go`），
>   而不是罗克韦尔的完整规范；`pack_test.go` 是 Logix UDT 打包的字节级基准。
> - Omron：Omron《NJ/NX-series CPU Unit Built-in EtherNet/IP Port User's Manual》W506-E1-31
>   **§7-6**（读写服务）、**§7-7**（变量数据类型）。获取方式见 `docs/resources.md`。
>
> **标注约定**：✅ = 有单测/代码明确保证；⚠️ = 文档与旧实现存在矛盾或尚未现场验证；
> ❌ = gologix 尚未实现（需要新方言）。

---

## 1. 速查矩阵

| # | 场景 | AB Logix（gologix 实现） | 汇川 AT_DEFAULT | 汇川 AT_INOPROSHOP（对齐参数=1） | Omron NX/NJ（W506 §7-7） |
|---|---|---|---|---|---|
| 1 | **顶层** 单个 BOOL | 1 字节 | **1 字节**，LSB 有效（§4.2） | 1 字节 | **2 字节**：`Status` + `Forced set/reset`（写时填 0） |
| 2 | **顶层** `BOOL[n]` 整体读写 | 读：按 **32 bool/DWORD** 位打包，`n` 必须是 32 的倍数；写：不可用 ⚠️ | **n 字节**（1 字节/元素，LSB）（§4.2） | n 字节 | **位打包进 WORD**（16 bit/WORD）：`ceil(n/16)*2` 字节 |
| 3 | **顶层** `BOOL[n]` 单个元素 `t[3]` | 1 字节 | **1 字节** | 1 字节 | **2 字节**（按单个 BOOL 格式）⚠️ |
| 4 | **成员** 单个 BOOL | 1 字节（与相邻 bool 共享位流，见 §3.4） | **2 字节**，2 字节对齐，LSB（§4.4.2(1)） | 1 字节 | **2 字节**（与顶层同规则，Omron 不分成员/顶层） |
| 5 | **成员** `BOOL[n]` 整体读写 | 1 bit/元素，与相邻 bool 成员共享位流，`ceil(n/8)` 字节 | 位紧密排列，总长**凑 16bit 倍数**（§4.4.2(2)） | **n 字节**（1 字节/元素） | **位打包进 WORD**（16 bit/WORD） |
| 6 | **成员** `BOOL[n]` 单个元素 `S.m[3]` | 1 字节 | **2 字节**（按成员 BOOL 规则）⚠️ | 1 字节 | **2 字节** ⚠️ |
| 7 | **结构体整体**传输中的 BOOL 成员 | 位打包 + 自然对齐 + **末尾不凑整** | 2 字节/成员 + C 自然对齐 + 整体凑整 | 1 字节 + 全 1 字节对齐 | ⚠️ 手册未给成员排布规则，未验证 |
| 8 | 结构体整体访问（gologix 接线状态） | ✅ 已接线（`Pack`/`Unpack` + `write_udt`，带 typecrc） | ❌ 未接线（显式报错 / `unknown type 0xA2`） | ❌ 同 | ❌ 无 Omron 方言 |

> **一句话记忆**：Logix 的 bool 是"**1 字节 + 数组按 32 位打包**"；汇川默认对齐的 bool 是
> "**顶层 1 字节、成员 2 字节**"，数组则是"**顶层按字节、成员按 16 位打包**"；
> Omron 则是"**单个值 2 字节（status+forced）、指定元素数按 1 字节/元素、整体数组按 16 位 WORD 打包**"。
> 三者**只在个别形状上巧合一致**，其余全部错位。
>
> ⚠️ **三家的规则轴完全不同**：Logix 看"是不是数组元素读写"、汇川看"**顶层标签 or 结构体成员**"、
> **Omron 看"这一请求是单个值、指定元素数的数组、还是整体数组"**——同一个 Omron 变量换个请求
> 方式就会得到不同格式。所以**不能**把某一家的规则简单套到另一家。Omron 细节见
> `omron-nx-nj-eip.md`。

---

## 2. 术语

| 术语 | 含义 |
|---|---|
| **顶层标签** | tag 路径不含 `.`，如 `MyBool`、`MyBoolArray`、`application__gvl__taga`。汇川 InoProShop/IFA 工程的全局变量列表需要 `application__gvl__tag` 前缀，那仍是**单个符号名**，算顶层。 |
| **结构体成员** | tag 路径含 `.`，如 `Stru.mem`、`Stru.flags[3]`（文档 §2.8：成员用 `.`，数组元素用 `[n]`）。 |
| **AT_DEFAULT** | 汇川 `TAG_AlignType = 0x00`，"EIP 协议默认对齐规则"。Easy 系列 / AutoShop 无法配置对齐参数，固定走这套。 |
| **AT_INOPROSHOP** | `TAG_AlignType = 0x01`，取决于后台建结构体时设的**对齐参数**；本文只覆盖"对齐参数 = 1"（全部 1 字节对齐、无任何填充）。 |
| **位序** | 本文的"低位在前"指 `bit i` 落在第 `i/8` 字节的 `bit i%8`（LSB 优先），即 `buf[i/8] |= 1 << (i%8)`。 |
| **类型码** | BOOL 恒为 `0xC1`（CIP）。写请求的 footer 带 `DataType` 与 `Elements`；读请求只带 `Elements`，类型由响应给出。 |

---

## 3. 逐场景详解

### 3.1 顶层单个 BOOL

| | Logix | 汇川 AT_DEFAULT | 汇川 AT_INOPROSHOP |
|---|---|---|---|
| 规则 | 1 字节，非 0 即 true | **1 字节，最低位有效**（§4.2） | 1 字节 |
| 读 | `readValue(0xC1)` 读 1 字节 | 同 | 同 |
| 写 | `binary.Write(bool)` = 1 字节 | 1 字节 | 1 字节 |
| 线上 | `01` / `00` | `01` / `00` | `01` / `00` |

**结论**：三套规则在顶层单个 BOOL 上**完全一致**，gologix 不需要任何特殊处理。
（旧的 `boolSize=2` 曾对顶层也写 2 字节，违反 §4.2，已在改造中移除。）

源码：`types.go` `readValue(CIPTypeBOOL)`、`write.go` `write_single`、`inovance_write.go`
`invBoolElementSize`（顶层返回 1）。

### 3.2 顶层 `BOOL[n]` 整体读写

| | Logix（gologix） | 汇川 AT_DEFAULT |
|---|---|---|
| 读 | 把 `BOOL[n]` 当 **DWORD 数组**读：请求 `n/32` 个 UDINT，每 DWORD 解出 32 个 bool（低位在前）；**`n` 必须是 32 的倍数**，否则直接报错 | 请求 `n` 个 BOOL 元素 → **返回 n 字节**，每字节 LSB 有效；**任意 n 都支持**（§4.2，文档举例 `Bool[17]` → 17 字节） |
| 写 | ⚠️ **不可用**：`GoVarToCIPType([]bool)` 落成 `0xA0`(STRUCT)，会发 `DataType=0xA0` + n 字节（每元素 1 字节），既不是 Logix 的位打包也不是汇川的字节序列 | 1 字节/元素，共 n 字节 |

**`n=32` 时的对照**（同一个 `BOOL[32]` 标签，同一批数据）：

```
值            bool[0]=T, bool[1]=F, ..., bool[8]=T, ...  （第 0、8 位为真）

Logix  读出 4 字节：  01 01 00 00     ← 位打包：bit i = byte[i/8] 的 bit(i%8)
汇川   读出 32 字节： 01 00 ... 01 ... ← 每元素 1 字节，只取 LSB
```

两者**完全不能互推**：Logix 的第 8 个 bool 在 `byte1` 的 bit0，汇川的在 `byte8` 的 bit0。

源码：`read.go` `ReadWithContext` 的 `case []bool:`（Logix 分支）、`inovance_read.go`
`invReadBoolSlice`/`invReadBoolBytes`（汇川分支）、`inovance_write.go` `invBoolArrayPayload`。

### 3.3 顶层 `BOOL[n]` 的单个元素（`MyBoolArray[3]`，即 go-iidc 的 `elementIndex`）

| | Logix | 汇川 AT_DEFAULT |
|---|---|---|
| 读 | 1 字节 | 1 字节 |
| 写 | 1 字节 | **1 字节**（顶层规则，非成员规则） |

⚠️ 这是与旧 `boolSize=2` 的**行为变化点**：旧实现对任何 BOOL 写都发 2 字节（包括顶层数组元素），
新实现按 §4.2 发 1 字节。按文档 1 字节才对，但需要现场确认（写错时 PLC 会回
`ERRR_WRITE_DATASIZE_UNCONSISTENT`，不会静默写坏）。

### 3.4 结构体成员单个 BOOL

| | Logix（gologix `Pack`） | 汇川 AT_DEFAULT | 汇川 AT_INOPROSHOP |
|---|---|---|---|
| 线上大小 | **1 字节**（低位有效），并与**相邻的 bool 成员共享同一条位流** | **2 字节**（§4.4.2(1)：按 2 字节对齐、占 2 字节、最低位有效） | 1 字节 |
| 字节形态 | `01` + 由**下一个成员的自然对齐**决定是否补填充 | `01 00`（第 2 字节是它的另一半，恒为 0） | `01` |
| 读 | `readValue(0xC1)` 读 1 字节（单值读只解析第 0 字节，LSB 在那里，值正确） | 同（多余字节不解析） | 同 |
| 逐元素读（多元素请求） | 每元素 1 字节 | 每元素 **2 字节**，需跳过 1 个 padding 字节（已实现，缺失会报错） | 1 字节 |

**Logix 的"共享位流"很关键**：`pack.go` 把连续的 bool 字段按 1 bit 依次塞进同一个累加字节
（`bitpack |= v << bitpos`），满 8 bit 或遇到非 bool 字段时落盘一字节。所以：

```
type S struct { A bool; B bool }     → 1 字节（A=bit0, B=bit1）
type S struct { A bool; B int32 }    → A 占 byte0，再补 3 字节填充，B 在 offset 4
```

而汇川 AT_DEFAULT 下：

```
{ A:BOOL; B:BOOL }   → A 占 byte0..1，B 占 byte2..3，共 4 字节（两个 2 字节成员）
```

源码：`pack.go` `Pack` 的 bool/bool-array 分支、`pack_test.go`；汇川侧 `inovance_write.go`
`invSerializeBool`、`inovance_read.go` `invBoolElementSize`/`invSkipBoolPadding`。

### 3.5 结构体成员 `BOOL[n]` 整体读写

| | Logix | 汇川 AT_DEFAULT | 汇川 AT_INOPROSHOP |
|---|---|---|---|
| 布局 | 1 bit/元素，**并入同一条位流**（可与前面的 bool 成员共用字节） | 位紧密排列、**低位在前**，总长**凑 16bit 倍数**（§4.4.2(2)） | **n 字节**（1 字节/元素，§4.4.2(2)） |
| 大小 | `ceil(n/8)` 字节（自身不凑整；下一个成员再按对齐补填充） | `roundUp(n,16)/8` 字节 | n 字节 |
| 对齐 | 元素类型（bool → 1） | **2 字节** | 1 字节 |
| gologix 读 | ⚠️ 成员 `[]bool` 读在 Logix 下走的是"顶层 32/DWORD"那条老路（要求 `n%32==0`），并非 UDT 内的 `ceil(n/8)` 位流——UDT 内的位流只在**结构体整体读**（`Pack`/`Unpack`）时生效 | `ceil(n/16)` 个 WORD（`0xD2`）→ 解位、**丢弃尾部 padding 位** | n 字节 |

**文档 §4.4.2(2) 的原例**：`Bool[16]` = 2 字节、`Bool[10]` = 2 字节、`Bool[17]` = 4 字节。

源码：`pack.go`（Logix 位流）、`inovance_read.go` `invReadBoolSlice`/`invReadBoolWords`/
`invUnpackBoolWords`、`inovance_write.go` `invBoolArrayPayload`。

### 3.6 结构体成员 `BOOL[n]` 的单个元素（`Stru.flags[3]`）

| | Logix | 汇川 AT_DEFAULT | 汇川 AT_INOPROSHOP |
|---|---|---|---|
| 写 | 1 字节 | **2 字节** | 1 字节 |
| 读 | 1 字节 | 2 字节（只解析第 0 字节即可得值） | 1 字节 |

⚠️ **未验证**：§4.4.2(2) 说成员数组是"位紧密排列"，那么"写单个元素"发 2 字节是否被设备接受
存疑——理论上可能需要"读包含该位的 WORD → 改位 → 写回"（非原子）。当前实现与旧 `boolSize=2`
一致（发 2 字节），若现场报长度错就按读改写实现。

### 3.7 结构体整体传输中的 BOOL / BOOL[n]

| | Logix | 汇川 AT_DEFAULT | 汇川 AT_INOPROSHOP |
|---|---|---|---|
| 成员 BOOL | 1 bit（位流） | **2 字节** | 1 字节 |
| 成员 `BOOL[n]` | 1 bit/元素，`ceil(n/8)` | 位打包，凑 16bit | n 字节 |
| 其它成员 | 自然对齐（INT 2 / DINT·REAL 4 / LINT 8） | C 自然对齐（§4.4.1） | 1 字节对齐（无填充） |
| 结构体整体 size | **不凑整**（末尾字段写多少就是多少） | 凑成"最大成员长度"的整数倍 | 各成员之和 |
| gologix 状态 | ✅ 已接线：`Pack`/`Unpack` + `write_udt`（`0xA0` + typecrc） | ❌ **未接线**（`PackInovance` 已实现未接；`write_udt` 显式拒绝；读回 `0xA2` → `unknown type`） | ❌ 同 |

**文档第 16/17 页的字节夹具**（`inovance_pack_test.go` 逐字节断言）：

```
UDT1 { m0:INT; m1:DINT }
  AT_DEFAULT      : 34 12 | 00 00 | 44 33 22 11          共 8 字节（DINT 4 字节对齐）
  AT_INOPROSHOP(1): 34 12 | 44 33 22 11                  共 6 字节（DINT 不做 4 字节对齐）

UDT2 { m0:BOOL; m1:BOOL[10]; m2:DINT }        （m1[0]、m1[8]、m1[9] 为真，m2 = 0x0A0B0C0D）
  AT_DEFAULT      : 01 00 | 01 03 | 0D 0C 0B 0A          共 8 字节
                    └m0┘   └─m1─┘   └──m2──┘
                    m0 = byte0 的 bit0，byte1 是它的第 2 字节
                    m1[0..7] = byte2 的 bit0..7；m1[8]、m1[9] = byte3 的 bit0、bit1
  AT_INOPROSHOP(1): 01 | 01 00 00 00 00 00 00 00 00 01 | 0D 0C 0B 0A   共 15 字节
                    └m0  └──────── m1[0..9]，每元素 1 字节 ────────┘   └─m2 未对齐─┘
```

---

## 4. 字节级示例对照（同一份数据，四种规则）

| 值 | Logix | 汇川 AT_DEFAULT | 汇川 AT_INOPROSHOP | Omron NX/NJ |
|---|---|---|---|---|
| 顶层 `bool = true` | `01` | `01` | `01` | `01 00`（Status + Forced） |
| 顶层 `BOOL[4] = T,F,F,T` 整体写 | ⚠️ 不可用 | `01 00 00 01` | `01 00 00 01` | `09 00`（位打包进 1 个 WORD） |
| 顶层 `BOOL[4]` 指定元素数=4 读 | — | 同左（n 字节） | 同左 | `01 00 00 01`（每元素 1 字节 Status） |
| 顶层 `BOOL[32]`（bit0、bit8 为真）整读 | `01 01 00 00`（4 字节） | 32 字节，`byte0=01`、`byte8=01` | 32 字节，同左 | `01 00 01 00`（2 个 WORD，各 bit0） |
| 顶层 `MyBoolArray[3] = true` | `01` | `01` | `01` | `01 00` |
| 成员 `bool = true` | `01`（+ 视后续成员补填充） | `01 00` | `01` | `01 00`（与顶层同规则） |
| 成员 `BOOL[8]`（bit0、bit7 为真） | `81`（1 字节） | `81 00`（16bit 凑整 → 2 字节） | 8 字节：`01 00 00 00 00 00 00 01` | `81 00`（位打包进 1 个 WORD） |
| 成员 `BOOL[10]`（m0、m9 为真） | `01 02`（`ceil(10/8)`=2 字节） | `01 02`（凑 16bit = 2 字节，**恰好一致**） | 10 字节：`01 00 … 00 02` | `01 02`（位打包，**与 Logix/汇川默认一致**） |
| 成员 `BOOL[16]`（bit0、bit15 为真） | `01 80`（2 字节） | `01 80`（**恰好一致**） | 16 字节 | `01 80`（**一致**） |
| 成员 `BOOL[17]`（bit0 为真） | `01 00 00`（3 字节） | `01 00 00 00`（凑 32bit = 4 字节） | 17 字节 | `01 00 00 00`（2 个 WORD，**与汇川默认一致**） |
| 结构体 `{bool; DINT}` | `01 00 00 00 \| dd cc bb aa`（bool 1 字节 + 3 填充） | `01 00 \| 00 00 \| dd cc bb aa`（bool 2 字节 + 2 填充） | `01 \| dd cc bb aa`（共 5 字节） | ⚠️ 成员排布未验证 |
| 结构体 `{bool; SINT}` | `01 \| ss`（共 2 字节） | `01 00 \| ss`（共 3 字节） | `01 \| ss`（共 2 字节） | ⚠️ 同上 |
| 结构体 `{bool; bool}` | `01`（两个 bool 共享一个字节的 bit0/bit1） | `01 00 00 00`（4 字节） | `01 01` | ⚠️ 同上 |

> 观察一：Logix 与汇川 AT_DEFAULT **只在 `ceil(n/8)` 为偶数的 `BOOL[n]` 成员上巧合一致**
> （`Bool[10]`、`Bool[16]`），其余（`Bool[8]`、`Bool[17]`、连续 bool、`{bool; SINT}`）都会错位。
> 这解释了现场"有些点正常、有些点错位"的现象。
>
> 观察二：**Omron 的"数组整体访问"（16 bit/WORD 位打包）与汇川 AT_DEFAULT 的成员数组规则
> 恰好是同一套**（`Bool[10]`/`Bool[16]`/`Bool[17]` 全对得上）；再叠加两者"单个 bool 都是
> 2 字节"的表象，就很容易被误认为"同一套规则"。实际成因不同（Omron 是 status+forced，
> 汇川是 2 字节对齐），且**顶层数组**规则截然不同（Omron 位打包 vs 汇川 n 字节）。

---

## 5. gologix API 行为矩阵

| API | Logix | 汇川 AT_DEFAULT | 汇川 AT_INOPROSHOP | Omron NX/NJ |
|---|---|---|---|---|
| `Read(tag, *bool)` | 1 字节 | 1 字节（只解析第 0 字节，LSB） | 1 字节 | ❌ 未实现：需要读 2 字节（status + forced），当前只解析 1 字节，值仍是对的但会剩 1 字节 |
| `Write(tag, bool)` | 1 字节 | 成员 tag → **2 字节**；顶层 tag → 1 字节 | 1 字节 | ❌ 未实现：**必须写 2 字节**（`{status, 0x00}`）。这正是旧 `boolSize=2` 在做的事 |
| `Read(tag, []bool)` | `n/32` 个 DWORD，**要求 `n%32==0`** | 成员 → `ceil(n/16)` 个 WORD；顶层 → n 字节 | n 字节 | ❌ 未实现：整体访问要按 **16 bit/WORD** 解位（与旧 `boolSize=2` 的 `[]bool` 分支一致）；指定元素数则每元素 1 字节 |
| `Write(tag, []bool)` | ⚠️ 不可用（类型落成 `0xA0`） | 成员 → 位打包凑 16bit；顶层 → n 字节 | n 字节 | ❌ 未实现：整体按 16 bit/WORD 位打包；指定元素数则每元素 1 字节 `Status` |
| `ReadList`/`ReadMap`（`TagType=BOOL`, `Elements>1`） | 每元素 1 字节 | 成员 → 每元素 2 字节（跳过 padding，缺失报错）；顶层 → 1 字节 | 1 字节 | ❌ 未实现（Omron 指定元素数时是每元素 1 字节，反而与 Logix 相同） |
| 结构体整体 `Write(tag, struct)` | `Pack` + `TypeEncode` typecrc + `0xA0` | ❌ 显式报错（未接线） | ❌ 显式报错 | ❌ 未实现；且写请求要带 **Omron 自己的结构体 CRC**（`TypeEncode` 是 Rockwell CRC，不通用） |
| 结构体整体 `Read(tag, &struct)` | `Unpack` | ❌ 响应 `0xA2` → `unknown type` | ❌ 同 | ❌ 未实现（响应形状与 Logix 同为 `A0 + 02 + CRC`，可复用 `cipStructHeader`） |
| 结构体打包函数 | `Pack`/`Unpack`（位流 + 自然对齐） | `PackInovance`/`UnpackInovance`（已实现、**未接线**） | 同左 | 无 |

"成员 vs 顶层"的判定：`invIsMemberTag`（tag 路径含 `.` 即成员）。注意 `Stru.flags[3]`
算成员，`MyBoolArray[3]` 算顶层。**这条判定只对汇川有效**：Omron 不看成员/顶层，
Logix 不看层级。Omron 尚未实现方言，上表的 "❌" 表示需要新增 `DialectOmron` 才能用。

---

## 6. 已知坑与待验证项

| # | 项 | 说明 |
|---|---|---|
| 1 | ⚠️ Logix `Write(tag, []bool)` 不可用 | `GoVarToCIPType` 没有 `[]bool` 分支，落到 `case interface{}` → `0xA0`(STRUCT)，于是发出 `DataType=0xA0` + n 字节。需要写 BOOL 数组时，Logix 侧目前只能逐元素写（`tag[i]`）或按存储布局用 `[]uint32`/`[]byte` 整写（后者与 PLC 端存储布局是否匹配未在库内验证） |
| 2 | ⚠️ 汇川顶层 bool 写：1 字节 vs 旧 `boolSize=2` 的 2 字节 | 按 §4.2 是 1 字节；旧实现按 2 字节。需现场确认（写错会回 `ERRR_WRITE_DATASIZE_UNCONSISTENT`，不静默） |
| 3 | ⚠️ 汇川顶层 `BOOL[n]` 整读：n 字节 vs 旧实现的 16/WORD 位打包 | 按 §4.2 是 n 字节；旧实现把 §4.4.2 的**成员**规则套到了顶层。需现场确认 |
| 4 | ⚠️ 汇川成员 `BOOL[n]` 逐元素写 2 字节 | §4.4.2(2) 说成员数组位紧密排列，单元素写是否需读改写 WORD 未验证 |
| 5 | ⚠️ 成员 `BOOL[n]` 逐元素读 2 字节 + padding | 依 §4.4.2(1) 推断，与旧实现一致；padding 缺失时会**报错**而不是错位 |
| 6 | 对齐规则必须与 PLC 端一致 | 两套规则不可混用（文档 §7.13）；PLC 端切换对齐方式后需 Run/Stop 重启（§7.14） |
| 7 | 结构体整体访问未接线 | 汇川侧 `PackInovance` 已实现未接；成员访问不需要它；误用会响亮报错 |
| 8 | ⚠️ "Omron 也是 2 字节 bool" 是**结论对、原因错** | Omron 的 2 字节 = `Status` + `Forced set/reset`（写时填 0），**不是 2 字节对齐**；且它的数组规则按"请求形态"分三种。旧的 `boolSize=2` 实际上实现了 **Omron** 规则（scalar 2 字节、`[]bool` 16/WORD 位打包）、却被标成汇川用途。详见 `omron-nx-nj-eip.md` |
| 9 | Omron 连接尺寸上限 | §7-1-2：`Large_Forward_Open` 在 NJ/NX（非 NX701）上最大 **1994 B**；gologix 默认 `ConnectionSize=4000`（`connect.go:16`），**需要下调** |

---

## 7. 源码索引

| 关注点 | 位置 |
|---|---|
| Logix `[]bool` 读（32/DWORD） | `read.go` `ReadWithContext` → `case []bool:` |
| Logix UDT 位流打包 | `pack.go` `Pack`/`Unpack`（bool 与 bool 数组分支）、`cipPack.Align` |
| Logix UDT 字节基准 | `pack_test.go` `TestPack` |
| 方言与对齐配置 | `dialect.go`（`Dialect`、`InovanceAlign`、`InovanceOptions`、`UseInovance`） |
| 汇川类型码与归一化 | `inovance_types.go` |
| 汇川结构体布局引擎（未接线） | `inovance_pack.go`、夹具 `inovance_pack_test.go` |
| 汇川 BOOL 读 | `inovance_read.go`（`invReadBoolSlice`/`invBoolElementSize`/`invSkipBoolPadding`/`invUnpackBoolWords`） |
| 汇川 BOOL 写 | `inovance_write.go`（`invSerializeBool`/`invBoolArrayPayload`） |
| 接线点 | `read.go`（`[]bool` 与多元素 BOOL 循环）、`write.go`（`write_single`、`write_udt`） |
| 行为单测 | `inovance_wiring_test.go`、`inovance_boolread_test.go` |
| 成员/数组判定与大小写 | `inovance_read.go` `invIsMemberTag`、`ioi.go` `newIOI`（`KeepTagCase`） |
| Omron 差异（连接尺寸、寻址、结构体 CRC、STRING 空白） | `docs/omron-nx-nj-eip.md` |
