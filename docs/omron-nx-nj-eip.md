# Omron NJ/NX 系列的 EtherNet/IP 差异（对照 Logix / 汇川）

> **本文定位**：整理 Omron NJ/NX 系列（内置 EtherNet/IP 端口）在 **CIP 显式消息访问变量** 时与
> AB Logix、汇川不同的地方，供将来实现 `DialectOmron` 或排查现场问题时参考。
> BOOL 的逐场景对照见 `bool-handling.md`（本文只讲差异，不重复那张表）。
>
> **依据**：Omron《NJ/NX-series CPU Unit Built-in EtherNet/IP Port User's Manual》
> **Cat. No. W506-E1-31**，第 7 章 CIP Message Communications。下载地址见 `resources.md`
> （2026-09 起为 `files.omron.eu/.../w506_nj_nx-series_...pdf`，原 `assets.omron.eu` 链接已 404）。
> gologix 侧结论来自本仓库源码，已标注文件名/行号。
>
> **标注**：✅ = 有手册原文/代码依据；⚠️ = 手册未写明或存在冲突，需实机确认。

---

## 0. 先纠正一条流传的说法

`BoolSize=2` 那个旧开关的注释写的是"Omron NJ/NX 用 2 字节对齐、只有 LSB 有效"。
**结论对（单个 BOOL 确实要 2 个数据字节），原因错**：Omron 的 2 字节是
**`Status` 字节 + `Forced set/reset information` 字节**（写的时候强制字节必须填 0），
**和"对齐"无关**；而且 Omron 的数组规则是"按请求形态二选一"（见 §2），
与汇川"顶层 1 字节 / 成员 2 字节对齐"是**两套独立机制**。
换句话说：旧的 `BoolSize=2` 实际上是**Omron 兼容模式**被误标成了汇川用途。

---

## 1. 与 Logix / 汇川**一致**的部分（好消息：路径层几乎不用改）

| 层 | Omron NJ/NX（W506 §） | gologix 现状 |
|---|---|---|
| 服务码 | 读变量 `0x4C` / 写变量 `0x4D`（§7-6-1、§7-6-2） | ✅ 相同 |
| 变量名段 | ANSI Extended Symbol Segment：`0x91` + 长度 + **UTF-8** 名称 + 奇数字节补 `0x00`（§7-4-5） | ✅ `ioi.go` `marshalIOIPart`：`segmentTypeExtendedSymbolic = 0x91`（`path.go:34`）+ 长度 + 名称 + 补 0 |
| 结构体成员 | 每层成员各发一个 `0x91` 段（`VarAA.MemB` → `91 05 "VarAA" 00` + `91 04 "MemB"`）（§7-4-5） | ✅ gologix 按 `.` 切段，每段一个 0x91 |
| 数组下标 | Member ID 逻辑段：`0x28` + 1 字节（0–255）；`0x29` + `00` + UINT（0–65535）（§7-4-5） | ✅ `cipElement_8bit = 0x28` / `cipElement_16bit = 0x29`（`cip_definitions.go:95`） |
| 读请求数据 | `Num of Element`（UINT，2 字节）（§7-6-1） | ✅ `read.go` 读请求 `Serialize(ioi, elements)` |
| 读响应（非结构体） | `DataType` + `AddInfoLength(=00)` + 数据（§7-6-1） | ✅ 与 `msgCIPReadResultData{Type, Unknown}` 形状一致 |
| 读响应（结构体） | `A0`(缩写 STRUCT) + `AddInfoLength(=02)` + **结构体 CRC(2B)** + 数据（§7-6-1、§7-7-4） | ✅ 与 `cipStructHeader{StructTypeCRC uint16}` 形状一致 |
| 写请求 | `DataType` + `AddInfoLength` + `(AddInfo)` + `Num of Element` + 数据（§7-6-2） | ✅ 与 `msgCIPWriteIOIFooter{DataType, Elements}` 一致 |
| 多维数组顺序 | 从最深层元素开始（`Var[2][2]` → `[0][0],[0][1],[1][0],[1][1]`）（§7-7-4） | ✅ 与 Logix/汇川一致 |
| 消息类型 | UCMM（无连接）与 Class 3（连接）都支持（§7-1-2） | gologix 走 Class 3 |
| 类型码 | 与 CIP Common 基本一致：`C1` BOOL、`C2` SINT…`CB` LREAL、`D1` BYTE、`D2` WORD、`D3` DWORD、`D4` LWORD、`D0` STRING、`A0` 缩写 STRUCT、`A2` STRUCT、**`A3` ARRAY**（§7-7-1） | 大部分相同；`A3` 与下列厂商私有码 gologix 未使用 |

---

## 2. BOOL：三种形态，按**请求方式**区分（§7-7-3、§7-7-4）

| 请求形态 | 数据字节 | 说明 |
|---|---|---|
| 单个 BOOL 变量 | **2**：`Status`(01/00) + `Forced set/reset`(写时填 0) | 基础类型"Boolean Data"格式 |
| 指定 `Num of Element` 访问 BOOL 数组 | **n**：每元素 1 字节 `Status` | §7-7-4"Exceptions When Specifying the Num of Element Field" |
| **整体**访问 BOOL 数组（路径不带下标） | `ceil(n/16)*2`：**位打包进 WORD**，16 bit/WORD，未用位为 `rsv` | 手册例子 `BOOL b[2][3]` → 1 个 WORD |

⚠️ **`Num of Element = 1` 时两处描述冲突**：§7-6-1 说"指定 1 个元素时按该元素数据类型的格式"
（对 BOOL 即 2 字节），而 §7-7-4 的例外表是"每元素 1 字节 Status"。实机需确认。

⚠️ **与 Logix/汇川的结构性差异**：Omron 的规则**不看"顶层还是成员"**，而是看"**这一请求是
单个值、指定元素数的数组、还是整体数组**"。同一个 BOOL 变量，用不同请求方式会得到不同格式。
（Logix 看"是否元素读写"、汇川看"顶层/成员"，三家各不相同。）

---

## 3. 与 Logix / 汇川**不同**的部分

| # | 差异 | 手册依据 | 对 gologix 的影响 |
|---|---|---|---|
| 1 | **连接/消息尺寸上限** | §7-1-2：UCMM **502 B**；Class 3 用 `Forward_Open` **502 B**；`Large_Forward_Open`：**NX701 8192 B**，NX502/NX102/NX1P2/**NJ 全系 1994 B** | ⚠️ **必须改配置**：gologix 默认 `ConnectionSize = 4000`（`connect.go:16` `connSizeLargeDefault`）> 1994，NJ/NX 上要显式下调（NX701 除外） |
| 2 | **BOOL 三态** | §7-7-3/§7-7-4（见 §2） | 需要独立方言：scalar 2B（status+forced）、指定元素数 1B/元素、整体 16/WORD。**不能复用汇川那套** |
| 3 | **结构体写的 CRC** | §7-6-2/§7-7-4：写请求的 `AddInfo` = **结构体定义 CRC(2B)** | ⚠️ gologix 的 `TypeEncode`（`pack.go`）算的是 **Rockwell** CRC（例：Logix STRING = `0x0FCE`），**不通用**。要么实现 Omron 的 CRC 算法，要么只支持成员访问 |
| 4 | **原生 STRING** | §7-7-1 列了 `STRING = D0`，但 **§7-7-3 只文档化了定长 1/2/4/8 字节与 Boolean，没有 STRING 的数据格式**；全文唯一的 "Read STRING" 是 Controller 对象里 2+20 字节的机型名 | ⚠️ 原生 STRING 变量怎么读写**在 W506 里没有说明** → 与汇川一样，建议走字节数组（`ARRAY OF BYTE/SINT`） |
| 5 | **变量可访问性** | §7-2-2：`CIPUCMMRead`/`CIPRead` 等指令读的是"**a variable with a Network Publish attribute**"；错误码 `8007 An inaccessible variable was specified`（§7-6-1） | ⚠️ 客户端（PLC 读别人）明确要求 Network Publish；**服务端（外部客户端读 NJ/NX）在 §7-3 未明写这条**，需实机确认。实践上普遍要求"全局变量 + 网络公开" |
| 6 | **大小写** | W506 未声明变量名大小写敏感（只在登录名/密码处出现 case sensitive） | ⚠️ Sysmac Studio 变量名本身区分大小写，而 gologix `newIOI` **默认把 tag 全部小写** → Omron 也需要 `KeepTagCase`（参见 `dialect.go` 的同名选项） |
| 7 | **结构体成员排布** | §7-7 只讲整体访问带 CRC，**没有给成员对齐/填充规则** | ⚠️ 结构体内 BOOL 占几字节、成员如何对齐**未验证**（要查 Sysmac Studio 结构体规则或实测） |
| 8 | 厂商私有类型码 | §7-7-1：`04/05/06` BCD、`07` ENUM、`08–0B` `*_NSEC`、`0C` Union | 目前 gologix 不支持；按需再加 |
| 9 | 额外类型码 | `A3` = ARRAY（与 `A0` 缩写 STRUCT / `A2` STRUCT 并列） | gologix 未使用；响应里若出现 `A3` 会走 `readValue` 的 default 报错 |

---

## 4. 若将来实现 `DialectOmron`（改动清单）

| 优先级 | 项 | 说明 |
|---|---|---|
| P0 | `ConnectionSize` 限制 | 默认 4000 → NJ/NX 需 ≤1994（NX701 ≤8192）；建议做成方言默认值而不是让用户手改 |
| P0 | BOOL 三态 | scalar 2B（status+forced(=0)）；指定元素数按 1B/元素；整体数组按 16 bit/WORD 位打包（读与写都要） |
| P1 | `KeepTagCase` | 打开以保留变量名原始大小写 |
| P1 | 结构体整体访问 | **读**可复用现有 `cipStructHeader` 形状；**写**需要 Omron 的 CRC 算法（否则先只支持成员访问） |
| P2 | 原生 STRING | 先按"字节数组"方案（与汇川同为 `D0`，但手册没给格式） |
| — | 路径/服务码/响应形状 | **不需要改**（已与手册一致，见 §1） |

---

## 5. 未验证项（需实机或有 Omron 的人确认）

1. 外部客户端访问 NJ/NX 变量是否**强制 Network Publish**（§7-3 未写，§7-2 客户端侧写了）；
2. 变量名/成员名**大小写**敏感度；
3. `Num of Element = 1` 时 BOOL 是 2 字节还是 1 字节（手册两处冲突）；
4. 结构体成员的**对齐与 BOOL 成员字节数**；
5. 原生 `STRING` 变量的数据格式；
6. `Large_Forward_Open` 在 NJ/NX 上的实际协商行为（是否接受 4000 并协商成 1994）。

---

## 6. 参考与源码索引

| 关注点 | 位置 |
|---|---|
| 手册 | W506-E1-31 第 7 章：§7-1-2 尺寸上限、§7-2-2 指令/Network Publish、§7-3-2 变量访问消息结构、§7-4-5 变量名与下标、§7-5 对象服务、§7-6 读写服务、§7-7 数据类型 |
| BOOL 逐场景对照（三家） | `docs/bool-handling.md` |
| IOI/段构造 | `ioi.go` `newIOI`/`marshalIOIPart`、`path.go:34`、`cip_definitions.go:95-97` |
| 连接尺寸 | `connect.go:16` `connSizeLargeDefault = 4000`、`client.go` `ConnectionSize` |
| 读响应/结构体 CRC | `read.go` `msgCIPReadResultData`、`cipStructHeader` |
| 类型编码（Rockwell CRC） | `pack.go` `TypeEncode` |
| 方言机制（可扩展点） | `dialect.go`（`Dialect`/`InovanceAlign`/`UseInovance`）、`inovance_*.go` |
