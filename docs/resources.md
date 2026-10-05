
# Manufacturer Documentation and Resources:

- https://literature.rockwellautomation.com/idc/groups/literature/documents/pm/1756-pm020_-en-p.pdf
- https://www.odva.org/wp-content/uploads/2020/06/PUB00123R1_Common-Industrial_Protocol_and_Family_of_CIP_Networks.pdf
- https://scadahacker.com/library/Documents/ICS_Protocols/Rockwell%20-%20Communicating%20with%20RA%20Products%20Using%20EtherNetIP%20Explicit%20Messaging.pdf
- http://iatips.com/digiwiki/quick_eip_demo.pdf
- https://github.com/EIPStackGroup/OpENer/
- https://github.com/loki-os/go-ethernet-ip
- https://www.can-cia.org/fileadmin/resources/documents/proceedings/2005_schiffer.pdf
- https://github.com/ruscito/pycomm
- http://www.plctalk.net/qanda/showthread.php?t=133853
- https://www.odva.org/wp-content/uploads/2020/05/PUB00070_Recommended-Functionality-for-EIP-Devices-v10.pdf
- https://literature.rockwellautomation.com/idc/groups/literature/documents/qs/2080-qs002_-en-e.pdf
- https://www.rockwellautomation.com/content/dam/rockwell-automation/sites/downloads/pdf/TypeEncode_CIPRW.pdf
- https://files.omron.eu/downloads/latest/manual/en/w506_nj_nx-series_cpu_unit_built-in_ethernet_ip_port_users_manual_en.pdf
  — Omron NJ/NX-series CPU Unit Built-in EtherNet/IP Port User's Manual（Cat. No. W506）。
  旧的 `assets.omron.eu/.../v2/w506_nx_nj-series_...pdf` 链接已 404，2026-09 换成上面的地址。
  §7 CIP Message Communications 是 Omron 变量访问规则的出处，见 `omron-nx-nj-eip.md`。
- https://github.com/JeremyMedders/LogixLibraries
- https://github.com/mikeav-soft/LogixTool/tree/master
- https://www.automation-pros.com/enip1/UserManual.pdf
- https://rockwellautomation.custhelp.com/ci/okcsFattach/get/114390_5
- https://www.rockwellautomation.com/content/dam/rockwell-automation/sites/downloads/pdf/developerguide.pdf

# Local copies:

- `汇川EIP标签通信库使用说明V2.0.2.8.pdf` — 汇川（Inovance）EIP 标签通信库使用说明 V2.0.2.8
  （2026-01-27）。`DialectInovance` 的类型码、两套结构体对齐规则（§4.2/§4.4）与第 16/17 页的
  数据排布表都出自这里；`inovance_pack_test.go` 的字节夹具即按那两张表编写。
  ⚠️ **本地副本，不入库**（文档页脚声明"版权所有·严禁复制"，而本仓库公开）——见 `.gitignore`；
  克隆仓库后此文件不存在，需要时从汇川获取。
- `EtherNetIP Adapter Protocol API 12 EN.pdf` — ODVA EtherNet/IP Adapter 协议规范。