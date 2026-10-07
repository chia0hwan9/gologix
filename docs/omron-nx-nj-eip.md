# Omron NJ/NX 系列的 EtherNet/IP 差异（对照 Logix / 汇川）

> **本文定位**：整理 Omron NJ/NX 系列（内置 EtherNet/IP 端口）在 **CIP 显式消息访问变量** 时与
> AB Logix、汇川不同的地方，供将来实现 `DialectOmron` 或排查现场问题时参考。
> BOOL 的逐场景对照见 `bool-handling.md`（本文只讲差异，不重复那张表）。
>
> **依据**：Omron《NJ/NX-series CPU Unit Built-in EtherNet/IP Port User's Manual》
> **Cat. No. W506-E1-31**，第 7 章 CIP Message Communications。下载地址见 `resources.md`
> （2026-09 起为 `files.omron.eu/.../w506_nj_nx-series_...pdf`，原 `assets.omron.eu` 链接已 404）。
> gologix 侧结论来自本仓库源码（用函数名定位；行号仅为写作时的大致位置）。
>
> **标注**：✅ = 有手册原文/代码依据；⚠️ = 手册未写明或存在冲突，需实机确认；
> ❌ = gologix 尚未实现（需新方言）。
>
> **修订记录**：2026-09 按 review 修订——① 结构体 CRC 改为"算法统一、差异在成员编码"；
> ② 多维数组顺序从"一致"降级为 ⚠️ 待实测；③ 补连接期差异（连接路径 slot、连接尺寸、
> electronic key）与 Symbol Object 缺失；④ §5 未验证清单补全。

---

## 0. 先纠正一条流传的说法

`BoolSize=2` 那个旧开关的注释写的是"Omron NJ/NX 用 2 字节对齐、只有 LSB 有效"。
**结论对（单个 BOOL 确实要 2 个数据字节），原因错**：Omron 的 2 字节是
**`Status` 字节 + `Forced set/reset information` 字节**（写时强制字节填 0），**与"对齐"无关**；
数组规则则按"请求形态"分三种（细节见 §2）。也就是说：旧的 `BoolSize=2` 实际上是
**Omron 兼容模式**被误标成了汇川用途，而汇川那套（`DialectInovance`）是**另一套独立机制**。

---

## 1. 与 Logix / 汇川**一致**的部分（路径与数据形状层）

| 层 | Omron NJ/NX（W506 §） | gologix 现状 |
|---|---|---|
| 服务码 | 读变量 `0x4C` / 写变量 `0x4D`（§7-6-1、§7-6-2） | ✅ 相同 |
| 变量名段 | ANSI Extended Symbol Segment：`0x91` + 长度 + **UTF-8** 名称 + 奇数字节补 `0x00`（§7-4-5） | ✅ `ioi.go` `marshalIOIPart`（`segmentTypeExtendedSymbolic = 0x91`，见 `path.go`） |
| 结构体成员 | 每层成员各发一个 `0x91` 段（`VarAA.MemB` → `91 05 "VarAA" 00` + `91 04 "MemB"`）（§7-4-5） | ✅ gologix 按 `.` 切段，每段一个 0x91 |
| 数组下标 | Member ID 逻辑段：`0x28` + 1 字节（0–255）；`0x29` + `00` + UINT（0–65535）（§7-4-5） | ✅ `cipElement_8bit = 0x28` / `cipElement_16bit = 0x29`（`cip_definitions.go`）；>255 自动用 0x29 ✓（但"下标语义"另有问题，见 §3-7） |
| 读请求数据 | `Num of Element`（UINT，2 字节）（§7-6-1） | ✅ `read.go` 读请求 `Serialize(ioi, elements)` |
| 读响应（非结构体） | `DataType` + `AddInfoLength(=00)` + 数据（§7-6-1） | ✅ 与 `msgCIPReadResultData{Type, Unknown}` 形状一致 |
| 读响应（结构体） | `A0`(缩写 STRUCT) + `AddInfoLength(=02)` + **结构体 CRC(2B)** + 数据（§7-6-1、§7-7-4） | ✅ 与 `cipStructHeader{StructTypeCRC uint16}` 形状一致 |
| 写请求 | `DataType` + `AddInfoLength` + `(AddInfo)` + `Num of Element` + 数据（§7-6-2） | ✅ 与 `msgCIPWriteIOIFooter{DataType, Elements}` 一致 |
| 消息类型 | UCMM（无连接）与 Class 3（连接）都支持（§7-1-2） | gologix 只走 Class 3（UCMM 作为备选，见 §4） |
| 类型码 | CIP Common：`C1` BOOL、`C2` SINT…`CB` LREAL、`D0` STRING、`D1` BYTE、`D2` WORD、`D3` DWORD、`D4` LWORD、`A0` 缩写 STRUCT、**`A2` STRUCT**、**`A3` ARRAY**（§7-7-1） | 大部分相同；`A3`（CIP 标准类型，不是 Omron 私有）gologix 未使用 |

> 厂商私有类型码（§7-7-1）：`04/05/06` BCD、`07` ENUM、`08–0B` `*_NSEC`、`0C` Union —— 与上表
> 分开列出，它们才是 Omron 特有。

---

## 2. BOOL：三种形态，按**请求方式**区分（§7-7-3、§7-7-4）

| 请求形态 | 数据字节 | 响应/请求 DataType | 推荐请求构造（对齐 gologix） |
|---|---|---|---|
| 单个 BOOL 变量（标量，非数组） | **2**：`Status`(01/00) + `Forced set/reset`（写时填 0） | `C1` | `Read(tag, &bool)` / `Write(tag, bool)`，payload 写 `{status, 0x00}` |
| 指定 `Num of Element = n` 访问 BOOL 数组 | **n**：每元素 1 字节 `Status` | `C1` | 路径**带** `0x28/0x29` 起始下标 + footer `Elements = n`；每元素 1 字节 |
| **整体**访问 BOOL 数组（路径**不带**下标） | `ceil(n/16)*2`：**位打包进 WORD**，16 bit/WORD，未用位 `rsv` | `C1`（见下） | 路径不带下标 + footer `Elements = n`；按 16 bit/WORD 解位 |

⚠️ **响应 type code 的坑（与 review 的结论不同，以手册原文为准）**：
§7-7-4 的 `BOOL b[2][3]` 示例原文是
`USINT C1 hex (data type code for BOOL)` + `USINT 00 hex` + 一行标注为 `(WORD)` 的位打包数据，
即**响应 type 仍是 `C1`，不是 `D2`**；变化的是**数据的宽度与含义**（2 字节 packed WORD，
而不是标量那套 status+forced）。但 §7-6-1 的响应格式只列了 `DataType + AddInfoLength + 数据`，
并未说明"整体数组"会改变元素宽度——**两处描述并不完全自洽**，所以客户端不能只看 type code
推断元素宽度，必须结合"本次请求是整体还是指定元素数"。实机抓一次包即可定死。

⚠️ **`Num of Element = 1` 时两处描述冲突**：§7-6-1 说"指定 1 个元素时按该元素数据类型的格式"
（对 BOOL 即 2 字节 status+forced），而 §7-7-4 的例外表是"每元素 1 字节 Status"。实机需确认。

⚠️ **与 Logix/汇川的结构性差异**：Omron 的规则**不看"顶层还是成员"**，而是看"**这一请求是
单个值、指定元素数的数组、还是整体数组**"。同一个 BOOL 变量，用不同请求方式会得到不同格式。
（Logix 看"是否元素读写"、汇川看"顶层/成员"，三家各不相同。）

---

## 3. 与 Logix / 汇川**不同**的部分

### 3.1 连接期差异（第一次 `Connect()` 就会踩，优先级最高）

| # | 差异 | 依据 | 影响 |
|---|---|---|---|
| 1 | 🔴 **连接路径必须清空 slot** | Omron §7-3-1：对象用 Class/Instance 逻辑段（Message Router `20 02 24 01`）；NJ/NX 内置端口**没有 backplane/slot** | gologix `NewClient` 默认 `Controller.Path = ParsePath("1,0")`（backplane→slot 0），`newForwardOpenLarge/Standard` 把它拼在 `CipObject_MessageRouter + Instance(1)` 前面。**必须 `client.Controller.Path = &bytes.Buffer{}`**（`connect.go` 顶部注释已提示"some devices need an empty path"），否则连接失败或路由错误。**这是 P0，且优先级高于 BOOL 三态** |
| 2 | 🔴 **连接尺寸上限** | §7-1-2（手册 p.7-3）：UCMM **502 B**；Class 3 `Forward_Open` **502 B**；`Large_Forward_Open`：**NX701 8192 B**，NX502/NX102/NX1P2/NJ **1994 B** | gologix 默认 `ConnectionSize = 4000`（`connSizeLargeDefault`）→ 走 Large，超 1994 被拒；失败后**回退到 `connSizeStandardDefault = 511`**（`connect.go` `Connect()` 的回退逻辑）→ 511 仍 > Omron 的 502 → **连接彻底失败**。必须显式设 ≤1994（走 Large）或 ≤502（走 Standard）。⚠️ NX701 的 8192 待实测复核 |
| 3 | ✅ **Electronic Key 不是问题**（澄清 review 的疑虑） | gologix `cipForwardOpen` 结构体只有标准的 **Originator Vendor ID / Serial**，**没有** target electronic key（vendor/device type/product code/revision）段；且 `vendorIdDefault = 0x9999`（**不是** Rockwell 的 1，注释写明"防止 vendor ID 冲突"） | 连接路径里只有 `Controller.Path + MessageRouter/1`，不带 key → Omron 不会因 key 不匹配拒绝。**无需改动**；实测时仍建议看一眼 Forward_Open 是否被拒及原因码 |
| 4 | ⚠️ UCMM 可绕开 1、2 | §7-1-2 支持 UCMM | UCMM 无连接、无需 `Forward_Open`（也就没有 slot path 与 1994 协商问题），只是每条消息上限 502 B。作为**降低连接期风险的替代实现路径**值得考虑（见 §4） |

### 3.2 数据层差异

| # | 差异 | 手册依据 | 对 gologix 的影响 |
|---|---|---|---|
| 5 | **BOOL 三态** | §7-7-3/§7-7-4（见 §2） | 需要独立方言：scalar 2B（status+forced）、指定元素数 1B/元素、整体 16 bit/WORD。**不能复用汇川那套** |
| 6 | **结构体 CRC**（按 review 修正） | §7-6-2/§7-7-4：写请求 `AddInfo` = 结构体定义 CRC(2B)；CIP 规范统一定义了该 CRC 算法 | ⚠️ **原"Rockwell CRC 不通用、必须重写算法"的说法过强**：算法是 CIP 统一的，gologix `pack.go` `TypeEncode` 的算法可复用；差异来自**成员类型编码的输入**——Logix 的 STRING 是"4 字节长度 + SINT[82]"（偏离标准 CIP STRING 的 UINT 长度 + 数据），打包 BOOL 数组的编码也可能与 Omron 不同。→ **纯数值成员的结构体，现有 CRC 大概率直接可用**；含 STRING / 打包 BOOL 的结构体才需要对齐编码。**先实测，别先假定要重写算法**（这条把 P1 工作量从"新 CRC"降为"复用 + 调整成员编码"） |
| 7 | **数组下标语义未定义** | §7-4-5 只说"用 Member ID 段填下标" | Omron 允许 `ARRAY[1..10]`（非 0 起始），Logix 一律 0 起始。段里填的是**声明的下标值**还是**从 0 起的偏移**，手册未说 → 典型的"Logix 直觉会做错"，进 §5 验证清单 |
| 8 | **原生 STRING 无格式** | §7-7-1 列了 `STRING = D0`，但 **§7-7-3 只文档化定长 1/2/4/8 字节与 Boolean**；全文唯一 "Read STRING" 是 Controller 对象里 2+20 字节机型名 | 原生 STRING 变量读写格式**手册空白** → 与汇川方案对齐：PLC 侧声明 `ARRAY[0..n] OF BYTE`，gologix 用 `[]byte` 读写 + 应用层拼串（推荐路径，见 §4） |
| 9 | **变量可访问性** | §7-2-2：客户端指令读的是"a variable with a **Network Publish** attribute"；错误码 `8007 An inaccessible variable was specified` | ⚠️ 客户端（PLC 读别人）明确要求；**服务端（外部客户端读 NJ/NX）§7-3 未明写** → 实测。实践上普遍要求"全局变量 + 网络公开" |
| 10 | **大小写** | W506 未声明变量名大小写敏感 | ⚠️ Sysmac Studio 变量名区分大小写，而 gologix `newIOI` **默认把 tag 全部小写** → Omron 也需要 `KeepTagCase`（`dialect.go` 已有该选项） |
| 11 | **结构体成员排布** | §7-7 只讲整体访问带 CRC，**没给成员对齐/填充规则** | ⚠️ 结构体内 BOOL 占几字节、成员如何对齐**未验证**（查 Sysmac Studio 结构体规则或实测） |
| 12 | **多维数组元素顺序**（按 review 从"一致"降级） | §7-7-4：`Var[2][2]` → `[0][0],[0][1],[1][0],[1][1]`，即**最后一维变化最快**（row-major） | ⚠️ **Logix 的顺序未验证**：社区有"Logix 多维数组第一维变化最快（column-major）"的说法，若属实则两者相反、整读会**静默错位**。gologix 侧只有"按最后一维递增做元素展开"（`taggroup.go` `expandArrayTag`、`datatable.go` 的 `EXAMPLE[1,0]`→`[1,4]`），那是**寻址**约定，不代表整读的字节顺序。→ 单列进 §5，用"写互异值 + 整读回读"实测 |
| 13 | **无 Symbol Object（0x6B）** | §7-5 列出的对象只有 Identity(`01`)、NX Configuration(`74`)、TCP/IP Interface(`F5`)、Ethernet Link(`F6`)、Controller(`C4`) | ⚠️ gologix 的 `ListAllTags`/`ListSubTags`/`ListMembers` 走 `CipObject_Symbol = 0x6B`（Logix 的符号对象）→ **Omron 上不可用**，tag 枚举/类型发现需另找途径（或由配置显式给出） |
| 14 | **错误码映射** | §7-6-1/§7-6-2 的响应码表：`05 PATH_DESTINATION_UNKNOWN`、`11 REPLY_DATA_TOO_LARGE`、`13/15` 长度不符、`1F`+`8007/8031/0104/1103`、`20`+`8017/8018/8023-8028` | gologix 的错误翻译层是按 Logix 写的 → 需要一张 Omron 对照表（P2） |
| 15 | 厂商私有类型码 | §7-7-1：BCD `04/05/06`、ENUM `07`、`*_NSEC 08–0B`、Union `0C` | 暂不支持，按需再加 |

---

## 4. 若将来实现 `DialectOmron`（改动清单，按"何时会踩"排序）

| 优先级 | 项 | 说明 |
|---|---|---|
| **P0-连接** | 清空 `Controller.Path` | NJ/NX 无 slot，连接路径只到 Message Router；建议在方言里默认置空，而不是让用户记得改 |
| **P0-连接** | `ConnectionSize` | 默认 4000 → 必须 ≤1994（Large）或 ≤502（Standard）；注意 gologix 的失败回退是 511，仍超 Omron 的 502，所以**不能靠回退**。建议方言默认值直接给 1994（NX701 可 8192，待实测） |
| **P0-数据** | BOOL 三态 | scalar `{status, 0x00}`；指定元素数 1B/元素；整体数组 16 bit/WORD 位打包（读与写都要） |
| P1 | 结构体读写 | **读**：复用现有 `cipStructHeader` 形状。**写**：先复用 `TypeEncode` 实测；纯数值成员大概率直接可用，含 STRING/打包 BOOL 再调成员编码 |
| P1 | `KeepTagCase` | 保留变量名原始大小写 |
| P1 | 多维数组顺序 | 实测后决定整读是否需要重排（会静默错位，风险高于报错类问题） |
| P2 | tag 枚举 / 类型发现 | 0x6B 不可用；要么按配置显式给类型，要么找 Omron 的替代对象（未验证） |
| P2 | 错误码对照表 | Omron 扩展状态码 → gologix 错误文案 |
| P2 | 原生 STRING | 按"字节数组"方案（`ARRAY[0..n] OF BYTE` + `[]byte` + 应用层拼串），与汇川一致 |
| 备选 | **UCMM 实现路径** | 用 UCMM 代替 Class 3 可绕开 slot path 与连接尺寸协商（代价：单消息 ≤502 B）。值得作为"连接期风险最小"的备选方案评估 |
| — | 路径段/服务码/响应形状 | **不需要改**（已与手册一致，见 §1） |

---

## 5. 未验证清单（需实机或有 Omron 的人确认）

| # | 项 | 验证方法 |
|---|---|---|
| 1 | **连接路径**：清空 `Controller.Path` 后能否连上；不清空时 Omron 返回什么错 | 改前后各连一次，看 Forward_Open 响应 |
| 2 | **连接尺寸**：Omron 对超限 `Forward_Open` 是**原样接受后报 oversized**还是主动协商降级？NX701 的 8192 是否属实 | 分别用 4000 / 1994 / 502 连接，观察实际返回与日志 |
| 3 | **Electronic Key**：gologix 不带 key（已确认），Omron 是否接受 `OriginatorVendorId = 0x9999` | 看 Forward_Open 是否成功及错误码 |
| 4 | **BOOL 整体数组的响应 DataType**：`C1` + WORD 打包（手册示例）还是别的；`Num of Element=1` 是 2 字节还是 1 字节 | 抓包对比两种请求的响应 |
| 5 | **Status 字节写入非 0/1 值**的行为（手册只定义 00/01） | 写 0x02/0xFF 看是否被拒或归一化 |
| 6 | **多维数组元素顺序**（Omron 与 Logix 是否相反） | 写互异值 → 整读 → 比对顺序 |
| 7 | **非 0 起始数组下标**（`ARRAY[1..10]`）：段里填声明下标还是 0 基偏移；>255 是否必须用 0x29 | 用 `ARRAY[1..10]` 变量试读第 1/2 个元素 |
| 8 | **外部客户端访问是否强制 Network Publish** | 用未公开的全局变量试读，看是否回 `8007` |
| 9 | **变量名/成员名大小写**敏感度 | 用大小写变体试读 |
| 10 | **结构体成员排布**（尤其成员 BOOL 字节数）与**结构体 CRC 是否与 `TypeEncode` 一致** | 对纯数值成员结构体整读/整写；再对含 STRING 的试 |
| 11 | **原生 STRING 变量**的读写格式 | 有原生 STRING 变量时抓包 |

---

## 6. 参考与源码索引

| 关注点 | 位置 |
|---|---|
| 手册 | W506-E1-31 第 7 章：§7-1-2 尺寸上限（p.7-3）、§7-2-2 指令/Network Publish、§7-3-1/7-3-2 变量访问消息结构、§7-4-5 变量名与下标、§7-5 对象服务（对象清单）、§7-6 读写服务与响应码、§7-7 数据类型 |
| BOOL 逐场景对照（三家） | `docs/bool-handling.md` |
| 连接路径 / Forward_Open | `connect.go`：`Connect()`（默认 path、Large→Standard 回退）、`newForwardOpenLarge`/`newForwardOpenStandard`（`Controller.Path` + `CipObject_MessageRouter`）、`cipForwardOpen` 结构体；`client.go` `NewClient`（`ParsePath("1,0")`） |
| 连接尺寸常量 | `connect.go`：`connSizeLargeDefault`/`connSizeStandardMax`/`connSizeStandardDefault` |
| IOI/段构造 | `ioi.go` `newIOI`/`marshalIOIPart`、`path.go`（`segmentTypeExtendedSymbolic`）、`cip_definitions.go`（`cipElement_8bit/16bit`） |
| 读响应/结构体 CRC | `read.go` `msgCIPReadResultData`、`cipStructHeader` |
| 类型编码（CIP CRC 算法） | `pack.go` `TypeEncode` |
| 符号对象枚举（Omron 不支持） | `listalltags.go`/`listsubtags.go`/`listmembers.go`（`CipObject_Symbol = 0x6B`） |
| 方言机制（可扩展点） | `dialect.go`（`Dialect`/`InovanceAlign`/`UseInovance`/`KeepTagCase`）、`inovance_*.go` |
