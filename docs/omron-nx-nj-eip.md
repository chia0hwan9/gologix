# Omron NJ/NX 系列的 EtherNet/IP 差异（对照 Logix / 汇川）

> **本文定位**：整理 Omron NJ/NX 系列（内置 EtherNet/IP 端口）在 **CIP 显式消息访问变量** 时与
> AB Logix、汇川不同的地方，供将来实现 `DialectOmron` 或排查现场问题时参考。
> BOOL 的逐场景对照见 `bool-handling.md`（本文只讲差异，不重复那张表）。
>
> **依据（两路，互相印证）**：
> 1. **手册**：Omron《NJ/NX-series CPU Unit Built-in EtherNet/IP Port User's Manual》W506-E1-31
>    第 7 章（§7-1-2 尺寸、§7-3-2 消息结构、§7-4-5 变量名、§7-6 读写服务、§7-7 数据类型）。
>    下载地址见 `resources.md`。
> 2. **官方实现**：OMRON SYSMAC Gateway / Compolet 的托管程序集（`SysmacCipService.dll`、
>    `CIPMessaging.dll`、`CIPObjectLibrary.dll`、`PLCDevice.dll`、`CompoletDataManager.dll`、
>    `SysmacCompolet.dll` 等 19 个），用 `ildasm` 反汇编后逐方法核对。**标注 `[DLL]` 的条目
>    有实现级证据**，具体方法与 IL 见 §6。
>
> **标注**：✅ 有手册/实现依据；⚠️ 未验证或存在冲突；❌ gologix 尚未实现（需新方言）。
>
> **修订记录**：2026-11 按 review 修订（CRC 表述、多维数组、连接期差异）；2026-11 追加
> **Compolet 程序集分析**——纠正了三条早期推断：CRC 由 PLC 提供而非本地计算、原生 STRING
> 有明确线格式、tag 枚举能力存在（TagInfo/TagName 服务）。

---

## 0. 先纠正一条流传的说法

`BoolSize=2` 那个旧开关的注释写的是"Omron NJ/NX 用 2 字节对齐、只有 LSB 有效"。
**结论对（单个 BOOL 确实要 2 个数据字节），原因错**：Omron 的 2 字节是
**`Status` 字节 + `Forced set/reset information` 字节**（写时强制字节填 0），**与"对齐"无关**。

`[DLL]` 这一点已由 OMRON 自己的实现确认：CIP 类型转换器里有一个 **`OmronBoolType`**（继承标准
`BoolType`），它的 `Size = 2`，`GetBytes(bool)` 分配 2 字节、只把 `[0]` 写成 0/1、`[1]` 保持 0
——即线上就是 `{status, 0x00}`。标准 `BoolType.Size = 1` 是给通用 CIP 设备用的。

也就是说：旧的 `BoolSize=2` 实际上是 **Omron 兼容模式**被误标成了汇川用途，而汇川那套
（`DialectInovance`）是**另一套独立机制**。

---

## 1. 与 Logix / 汇川**一致**的部分（路径与数据形状层）

| 层 | Omron NJ/NX | gologix 现状 |
|---|---|---|
| 服务码 | 读 `0x4C`、写 `0x4D`（§7-6；`[DLL]` `VariableClass::CIPWriteDataService` 里 `ldc.i4.s 77`） | ✅ 相同 |
| 变量名段 | ANSI Extended Symbol Segment：`0x91` + 长度 + **UTF-8** 名 + 奇数长度补 `0x00`（§7-4-5；`[DLL]` `SegmentTypes.ANSIExtendedSymbolSegment = 0x91`、`VariableRequestPath::AddVariable`） | ✅ `ioi.go` `marshalIOIPart` |
| 结构体成员 | 每层成员各一个 `0x91` 段（`Var.Mem` → 两个段）（§7-4-5；`[DLL]` `VariableRequestPath` 按 `.` 切分，逐段 `AddVariable`） | ✅ gologix 同 |
| 数组下标 | Member ID 逻辑段：`0x28`+1 字节 / `0x29`+`00`+UINT（§7-4-5；`[DLL]` `LogicalSegmentTypes.MemberID = 0x08` 与 `LogicalSegment = 0x20` 组合） | ✅ `cipElement_8bit/16bit` |
| 下标取值 | `[DLL]` 下标按 `UInt16.Parse` 取**字面声明的下标值**（不是 0 基偏移），多维每维一个段、按书写顺序 | ✅ gologix 解析方式相同（见 §3-7） |
| 读请求数据 | `Num of Element`(UINT)（§7-6-1） | ✅ 同 |
| 读响应（非结构体） | `DataType` + `AddInfoLength(=00)` + 数据 | ✅ 同 |
| 读响应（结构体） | `A0` + `AddInfoLength(=02)` + 结构体 CRC(2B) + 数据 | ✅ 与 `cipStructHeader` 形状一致 |
| 写请求 | `[DLL]` `ComposeRequestServiceData4D` 逐字节确认：`type(1) + addInfoLen(1) + [CRC(2) 仅当 addInfoLen==2] + elements(2) + [仅 0xD0: dataLen(2)] + data` | ✅ 与 `msgCIPWriteIOIFooter` 一致（STRING 的 dataLen 前缀 gologix 未实现） |
| 消息类型 | UCMM 与 Class 3 都支持（§7-1-2）；`[DLL]` 有 `ForwardOpen`(0x54) / `LargeForwardOpen`(0x5B) | gologix 走 Class 3 |
| 电子钥匙 | `[DLL]` 全部程序集里**没有 electronic key**；Forward_Open 只带 `TConnectionPath` + `TNetConnectionParameters`(RPI) | ✅ gologix 也不带（`vendorIdDefault = 0x9999`） |

---

## 2. BOOL：三种形态，按**请求方式**区分（§7-7-3、§7-7-4）

| 请求形态 | 数据字节 | DataType | 实现证据 |
|---|---|---|---|
| 单个 BOOL 变量 | **2**：`Status`(01/00) + `Forced set/reset`(0) | `C1` | `[DLL]` `OmronBoolType.Size=2`、`GetBytes` → `{0/1, 0}` |
| 指定 `Num of Element = n` 的 BOOL 数组 | **n**：每元素 1 字节 `Status` | `C1` | `[DLL]` `SysmacDataConverter.BoolValueByteArrToBoolArr`（1 字节/元素，`!=0` 即真） |
| **整体**访问 BOOL 数组 | `ceil(n/16)*2`：**位打包进 WORD**（16 bit/WORD） | **`0xFF`（不是 C1）** | `[DLL]` `CipOmronDataConverter.BoolsToBitArray`：当 `IsArray && Type==0xC1` 时**把请求类型改写成 0xFF**；`BitArrayToWordData`/`WordDataToBitArray` 做 16 位打包 |

⚠️ 上表第三行是最容易踩的：**客户端主动把类型写成 0xFF**，而不是 0xC1——这样 PLC 才按位打包
返回。gologix 的 Omron 方言已按此实现：整体读按**应答长度自适应**（n 字节=每元素 1 状态字节 /
2n 字节=status+forced / `ceil(n/16)*2` 字节=16 位字位打包，其它长度明确报错），整体写发
`0xFF + addInfoLen 0 + elements + 位打包数据`。

⚠️ **0xFF 冲突坑**：本库把 `0xFF` 用作 Logix 的 STRING 标记（`CIPTypeSTRING = 0xFF`），共享的
写 footer 会对它发出 `A0 02 <CRC>` 结构体头。Omron 位数组必须**手写 footer**
（`FF 00 <elements:u16>`），否则线上会变成 Logix STRING 结构体写。

⚠️ `Num of Element = 1` 时 §7-6-1 与 §7-7-4 描述冲突（2 字节 vs 1 字节），实机需确认。

⚠️ 与 Logix/汇川的结构性差异：Omron 的规则**不看"顶层还是成员"**，而看"**单个值 / 指定元素数 /
整体数组**"。（Logix 看"是否元素读写"、汇川看"顶层/成员"，三家各不相同。）

---

## 3. 与 Logix / 汇川**不同**的部分

### 3.1 连接期差异（第一次 `Connect()` 就会踩）

| # | 差异 | 依据 | 影响 |
|---|---|---|---|
| 1 | 🔴 **连接路径必须清空 slot** | NJ/NX 无 backplane/slot；`[DLL]` Forward_Open 只带 `TConnectionPath` + RPI 参数（**确切字节在原生层拼装，托管程序集里看不到**，见 §6 末行） | gologix `NewClient` 默认 `Controller.Path = ParsePath("1,0")`，必须 `client.Controller.Path = &bytes.Buffer{}`（此时 Forward_Open 的 path 正好是 `20 02 24 01`） |
| 2 | 🔴 **连接尺寸上限** | §7-1-2：UCMM 502 B；Class 3 `Forward_Open` 502 B；`Large_Forward_Open`：NX701 8192 B，其余 1994 B | gologix 默认 4000 → Large 被拒 → 回退 511（仍 > 502）→ 连接失败。必须显式设 ≤1994 或 ≤502 |
| 3 | ✅ **无 Electronic Key** | `[DLL]` 无该字段 | 无需改动 |
| 4 | ⚠️ **UCMM 可用，且是最简路径** | §7-1-2 支持 UCMM；`[DLL]` `OmronEipReader.dll` 的 `EipClient` **只走 UCMM**（`CMD_SEND_RR = 0x006F`，无 Forward_Open） | 可绕开 slot path 与 1994 协商（代价：单消息 ≤502 B）。实现成本最低，见 §6.2 |

### 3.2 数据层差异

| # | 差异 | 依据 | 影响 |
|---|---|---|---|
| 5 | **BOOL 三态** | §2 | 需独立方言，**不能复用汇川那套** |
| 6 | **结构体 CRC 由 PLC 提供，不是算出来的** | `[DLL]` 写请求用 `TagInfo.StructCrcCode`；`VariableTypeInstance` 从 PLC 属性流里解析出 `成员数(2) → nextID(2) → 嵌套类型ID(2) → CRC(2) → 类型名(1+UTF-8)`；`CIPUtil_CalculateCRC16` 是 P/Invoke 到原生 `CIPCoreWin32.dll`、**不走这条路** | ✅ **结论翻转**：gologix 不需要实现 CRC 算法，而是**读 PLC 的类型/标签信息拿 CRC**（`CipOmronTagInfoService`）。原"必须重写算法"作废 |
| 7 | **数组下标语义** | `[DLL]` 下标 = 字面声明值 | ✅ gologix 现有解析即正确；`ARRAY[1..10]` 用 `arr[1]` 访问 |
| 8 | **原生 STRING 有明确格式** | `[DLL]` 写请求：`0xD0` + addInfoLen=0 + elements(2) + **dataLen(2)** + data；`CipOmronWriteDataService` 对 `0xD0` 有单独分支 | ✅ 可实现（手册没写，但实现里有）。仍建议优先字节数组方案 |
| 9 | **结构体成员布局可从 PLC 查** | `[DLL]` `CipOmronTagInfoService::GetNestedInfo`：类型对象是一条**成员链**（`NestingVariableTypeInstanceID32` 起始，`NextInstanceID32` 迭代），每个成员带 name / DataType / NumberOfElements / StartArrayElements / **SizeInMemory**，**没有显式偏移** → 布局 = 按链顺序拼接各成员 SizeInMemory | ✅ 不用猜 Sysmac Studio 对齐规则 |
| 10 | **tag 枚举/类型发现存在** | `[DLL]` `UploadInfo` 的完整链路：`TagNameServer`（名→实例ID，客户端缓存 `mVariableIdCache`）→ `Variable` 实例（DataType / NumberOfElements / StartArrayElements / Size / VariableTypeInstanceID）→ 若 `0xA3` 数组则取 `DataTypeOfArray` → 若类型 ID≠0 再读 Variable Type 实例（CRC + 成员链） | ✅ 原"无 Symbol Object → 枚举不可用"作废：能力等价，只是对象/服务不同 |
| 11 | **变量可访问性** | §7-2-2 要求客户端读的远端变量有 Network Publish；§7-3 服务端未明写；错误码 `8007` | ⚠️ 实机确认 |
| 12 | **大小写** | W506 未声明；Sysmac Studio 变量名区分大小写 | ⚠️ 需 `KeepTagCase` |
| 13 | **多维数组元素顺序** | 手册：`Var[2][2]` → `[0][0],[0][1],[1][0],[1][1]`（最后一维最快） | ⚠️ Logix 侧顺序未验证，两者可能相反（静默错位） |
| 14 | **错误码映射** | §7-6 响应码表（`05`、`11`、`13/15`、`1F`+`8007/8031`、`20`+`8017/8018`…） | gologix 错误翻译层需 Omron 对照表（P2） |
| 15 | 厂商私有类型码 | §7-7-1：BCD `04/05/06`、ENUM `07`、`*_NSEC 08–0B`、Union `0C`；`[DLL]` 另有 **`0xFF` = 位数组** | `0xFF` 见 §2；其余按需 |

---

## 4. 若实现 `DialectOmron`（改动清单，按"何时会踩"排序）

| 优先级 | 项 | 说明 |
|---|---|---|
| **P0-连接** | 清空 `Controller.Path` | NJ/NX 无 slot，建议方言里默认置空 |
| **P0-连接** | `ConnectionSize` | 方言默认 1994（NX701 可 8192，待实测）；不能依赖 4000→511 的回退 |
| **P0-数据** | BOOL 三态 | scalar `{status,0x00}`；指定元素数 1B/元素；整体数组**类型改写 0xFF + 16 bit/WORD 位打包** |
| P1 | 结构体读写 | **读 PLC 类型信息拿 CRC 与成员布局**（`CipOmronTagInfoService`），不再实现 CRC 算法 |
| P1 | `KeepTagCase` | 保留变量名大小写 |
| P1 | 多维数组顺序 | 实测后决定整读是否重排（静默错位，风险高于报错类问题） |
| P2 | tag 枚举 | 实现 TagInfo/TagName 服务（替代 Logix 的 0x6B） |
| P2 | 原生 STRING | `0xD0 + dataLen(2) + data`（格式已明确）；仍建议优先 `ARRAY OF BYTE` |
| P2 | 错误码对照表 | Omron 扩展状态码 → gologix 错误文案 |
| **可选 P0** | **走 UCMM 而不是 Class 3** | 见 §6.2：`OmronEipReader.dll` 只用 UCMM 就能读写 NJ/NX 标量，**完全绕开连接路径与 1994 协商**（单消息 ≤502 B）。若目标是"先能读写"，这是成本最低的路线；Class 3 可后补 |
| — | 路径段/服务码/响应形状 | **不需要改** |

> **实现状态（2026-11）**：`DialectOmron`（Class 3）已落地——连接期（空连接路径 +
> `ConnectionSize` 1994）、单 BOOL 写 2 字节 `{status,0}`、整体 BOOL 数组（写 `0xFF` + 16 位字打包；
> 读按应答长度自适应解析）、tag 名保持大小写。**结构体读写明确拒绝**（读在发请求前就报错，因为
> Omron 的结构体响应同样是 `0xA0`，按 Logix 布局解包会静默返回错误数据）；尚未实现：结构体
> （需先实现 TagInfo 查询拿 CRC 与成员链）。入口：`client.UseOmron()` / `UseOmronWithOptions()`。

---

## 5. 未验证清单

| # | 项 | 验证方法 |
|---|---|---|
| 1 | 连接路径清空后能否连上；不清空时返回什么错 | 改前后各连一次 |
| 2 | Omron 对超限 `Forward_Open` 是拒绝还是协商；NX701 的 8192 是否属实 | 4000 / 1994 / 502 各试一次 |
| 3 | `Num of Element = 1` 的 BOOL 是 2 字节还是 1 字节 | 抓包对比 |
| 4 | `Status` 写入非 0/1 值的行为 | 写 0x02/0xFF |
| 5 | 多维数组元素顺序（Omron 与 Logix 是否相反） | 写互异值 → 整读 → 比对 |
| 6 | 外部客户端访问是否强制 Network Publish | 未公开的全局变量试读，看 `8007` |
| 7 | 变量名/成员名大小写敏感度 | 大小写变体试读 |
| 8 | 结构体成员排布（尤其成员 BOOL 字节数） | 读类型对象 + 实测整读 |
| 9 | `0xFF` 位数组在**读响应**里的类型码是否也是 0xFF | 抓包 |
| 10 | **连接路径的确切字节** | 托管层确认不了（原生 `CIPCorePort.dll` 负责）；只能实测或抓 OMRON 客户端与 PLC 的 Forward_Open 报文 |
| 11 | 成员 `SizeInMemory` 对 BOOL 成员是 1 还是 2 | 读一次结构体类型对象即可看到（`GetNestedInfo` 的成员链） |

---

## 6. 实现依据：OMRON Compolet 程序集分析

程序集位置（现场工程 `Sineva.PLCCommunication.SysmacGateway\lib`）：19 个托管 DLL。用
`ildasm.exe /out=<file>.il /utf8`（NETFX 4.8 Tools）反汇编，无需执行代码。

| 关注点 | 程序集 / 类型 / 方法 | 结论 |
|---|---|---|
| 变量读写服务 | `SysmacCipService`：`CipOmronReadDataService`、`CipOmronWriteDataService`、`CipOmronReadDataMultipleService` | NJ/NX 的 CIP 变量层；写服务码 0x4D（`ldc.i4.s 77`） |
| 段类型 | `CIPMessaging`：`SegmentTypes`、`LogicalSegmentTypes` | `0x91`；`MemberID=0x08`（→0x28/0x29） |
| 请求路径 | `CIPMessaging`：`VariableRequestPath`（`AddVariable`/`AddElementVariable`） | 按 `.` 切、每维 `[n]` 一个 MemberID 段，下标取字面值 |
| 写请求体 | `PLCDevice`：`VariableClass::ComposeRequestServiceData4D` | `type + addInfoLen + [CRC] + elements + [STRING 时 dataLen] + data` |
| 结构体 CRC 来源 | `CompoletDataManager`：`TagInfo.StructCrcCode`；`PLCDevice`：`VariableTypeInstance` 解析 | **CRC 来自 PLC 的类型/标签信息**；`CIPUtil_CalculateCRC16` 是原生 P/Invoke，不走此路 |
| 结构体写 | `SysmacCipService`：`CipOmronWriteDataService::FragmentWrite` | `IsStruct` → type=`0xA0`、addInfoLen=2、addInfo=`StructCrcCode` |
| BOOL 大小 | `CompoletDataManager`：`OmronBoolType`（`Size=2`，`GetBytes`→`{0/1,0}`） | 单 BOOL 2 字节 |
| BOOL 数组 | `CompoletDataManager`：`CipOmronDataConverter.BoolsToBitArray`；`SysmacCompolet`：`BitArrayToWordData`/`WordDataToBitArray`、`BoolValueByteArrToBoolArr` | 整体访问改写类型 **0xFF**、16 位打包；指定元素数 1 字节/元素 |
| 连接 | `CIPObjectLibrary`：`TConnectionPath`、`TNetConnectionParameters`、`ConnectionManagerInstance.ForwardOpen/LargeForwardOpen` | 无 electronic key；Forward_Open 0x54 / Large 0x5B |
| 标签信息链 | `SysmacCipService`：`CipOmronTagInfoService::UploadInfo` / `GetNestedInfo` / `GetVariableInstanceId` | `TagNameServer`→`Variable` 实例→（`0xA3` 取 `DataTypeOfArray`）→`VariableTypeClass` 实例（CRC + `NumberOfMember` + 成员链） |
| 连接路径为何拿不到 | `CIPPortCompolet`：`EIPPortInformation` 只有 `IPAddress`/`CardId`/`CardName`；`TConnectionPath` 只用于**解析**连接配置对象 | RegisterSession / Forward_Open 由**原生** `CIPCorePort.dll`/`CIPCoreWin32.dll` 完成（本目录没有这两个文件） |

> 说明：`VariableTypeInstanceCJ2`/`NE1S` 是 CJ 系列的解析路径；NJ/NX 走
> `CipOmronTagInfoService` + `SysmacCipChannel`，但两者都把 CRC 当作"从 PLC 读到的元数据"，
> 这一点一致。

### 6.1 现场实现参考（Sineva.PLCCommunication.SysmacGateway）

同目录下的网关工程（C# + OMRON Compolet，生产代码）对上面几条提供了**行为级旁证**：

| 事实 | 代码位置 |
|---|---|
| 单 bool 写 = **2 字节** `{1,0}` / `{0,0}` | `EIP1n3Compolet.cs`：`BIT_TRUE`/`BIT_FALSE` → `WriteSpecialSignal` → `remoteCompolet.WriteRawData(tag, buffer)` |
| 单 bool 读 ≥ 2 字节，判 `buffer[0] > 0 \|\| buffer[1] > 0` | 同上 `ReadSpecialSignal` |
| 连接用 **Class 3**、`ReceiveTimeLimit = 1000` | `remoteCompolet.ConnectionType = ConnectionType.Class3` |
| `Path destination unknown` 被当作"**tag 未注册**"而不是连接故障 | `catch` 里 `ex.Message.StartsWith("Path destination unknown")` → 记 "Tag Not Regist" |
| 每元素按 2 字节校验（`buffer.Length != size * 2`） | `EIPCommonCheck.cs`（注释代码） |
| **Connection Manager 扩展状态码表**（0x0100–0x08xx，52 条）可直接借用 | `SinevaLocalCompolet.cs` 的 `ConnectionStatusCode` 列表；如 `0x0109 Invalid Connection Size`、`0x0111 Rpi Not Supported`、`0x0114 Vendor Id Or Product Code Mismatch` |

注意：该网关自身**没有** CIP 报文级代码（Forward_Open/RegisterSession 全走 Compolet 的
`ReadRawData`/`WriteRawData`），所以连接路径的确切字节仍然拿不到。

### 6.2 参考实现：OmronEipReader.dll（纯 .NET，只走 UCMM）

第三方 `OmronEipReader.dll`（26 KB，`EipClient` + `CipService` + `CipDataTypeHelper` +
`OmronTagReader`）是一份**完整的报文级实现**，补上了网关和 Compolet 都没有的报文细节：

| 关注点 | 事实 |
|---|---|
| 传输方式 | **只用 UCMM**：`EipClient` 的常量只有 `CMD_REGISTER=0x0065`、`CMD_UNREGISTER=0x0066`、`CMD_SEND_RR=0x006F`、`HEADER_SIZE=0x18`；**没有 Forward_Open** |
| UCMM 帧（`SendCipMessage`） | 24 字节封装头 + CPF：`InterfaceHandle=0`(4) + `Timeout=0x000A`(2) + `ItemCount=2`(2) + `Null Address Item (0x0000, len 0)` + `Unconnected Data Item (0x00B2, len, CIP 请求)`；**无路由路径** |
| 读请求 | `0x4C` + `pathSize` + 路径 + `elements`(u16) |
| 写请求 | `0x4D` + `pathSize` + 路径 + **`dataType`(u16，高字节即 addInfoLen=0)** + `elements`(u16) + 数据 |
| 读响应 | `service` + `reserved` + `status` + `extSize`(1 字节，单位=字) + `extStatus`(extSize×2) + `type` + `[typeInfo]` + 数据；**先判 status 再解析数据** |
| **BOOL 写** | `EncodeValue(BOOL)` → **`new byte[2]`，`[0]=0/1`，`[1]=0`** —— 与手册、`OmronBoolType`、Sineva 网关**四方一致** |
| BOOL 解码 | `GetSize(BOOL)=1` —— 只用于**解码**（响应首字节就是 status，读 1 字节足够） |
| 路径 | `0x91` + 长度 + **ASCII** 名 + 奇数补 0；**不支持 `.` 成员与 `[n]` 下标**（整串当一个符号段） |
| 类型码 | `0xC1`–`0xCB`、`0xD0`、`0xD1`–`0xD4`、`0xDB`，与手册一致；`FromSysmacTypeName` 把 Sysmac 类型名映射成码 |
| 标签定义来源 | **不查 PLC**：`VariableFileParser.ParseFile` / `ParseCxDesignerTxt` 解析 **CX-Designer / Sysmac Studio 导出的制表符文本**（按 `RW`/`IN`/`OUT` 过滤） |
| 状态文案 | `GetCipStatusMessage`：0x00–0x26 的 CIP 状态文本表 |
| 不支持 | 结构体（无 addInfo/CRC）、成员/数组寻址、Class 3、TagInfo 服务 |

**对 gologix 的意义**：① 证明 NJ/NX 上 **UCMM 完全可用**，可把"连接期风险"直接绕掉；② BOOL
写 2 字节再获一重独立确认；③ 标签定义可以先从配置文件来（不必先实现 TagInfo 链）。

---

## 7. 参考与源码索引

| 关注点 | 位置 |
|---|---|
| 手册 | W506-E1-31 第 7 章（§7-1-2、§7-2-2、§7-3-2、§7-4-5、§7-5、§7-6、§7-7） |
| BOOL 三家对照 | `docs/bool-handling.md` |
| 连接路径 / Forward_Open | `connect.go` `Connect()`、`newForwardOpenLarge/Standard`、`cipForwardOpen`；`client.go` `NewClient` |
| 连接尺寸常量 | `connect.go` `connSizeLargeDefault` / `connSizeStandardMax` / `connSizeStandardDefault` |
| IOI/段构造 | `ioi.go`、`path.go`、`cip_definitions.go` |
| 读响应/结构体 CRC | `read.go` `msgCIPReadResultData`、`cipStructHeader` |
| 类型编码（CIP CRC 算法） | `pack.go` `TypeEncode` |
| CIP 错误分类 | `ciperror.go`、`errors.go` |
| 方言机制 | `dialect.go`（`DialectOmron`/`UseOmron`/`OmronOptions`）、`omron.go`、`inovance_*.go` |
