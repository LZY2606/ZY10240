# 田区空间校正台（Field Spatial Correction Workbench）

本地交付的育种田间试验空间校正工具：审查试验设计 → 在行列趋势 / 区组随机效应 /
局部相关三类可重放候选之间建立分析分支 → 将每个处理对比与实际使用的区组、
样本量和“校正前后差异”绑定展示。

- 语言/运行时：Go（仅标准库 + 纯 Go 的 `modernc.org/sqlite`，无需 CGO）
- 存储：本地 SQLite（默认 `fieldbench.db`）
- 页面：服务端渲染 SVG 田区网格 + 原生 HTML/JS 操作页
- 数据：随程序嵌入的固定 fixture `internal/fixtures/trial.csv`

## 安装与演示

```bash
go mod download
go test ./... -count=1
go run ./cmd/server --listen 127.0.0.1:5580
# 浏览器访问 http://127.0.0.1:5580 ，页面标题为“田区空间校正台”
```

可选参数：`--db 路径`（默认 `fieldbench.db`）。空数据库启动时自动导入固定
fixture；“清空并复核”按钮或 `POST /api/reset` 会清空全部表后重新导入。

## 固定样例（数据口径）

样例为 9 行 × 6 列、3 个区组（B1=行1–3，B2=行4–6，B3=行7–9），共 54 条记录：

- **重复坐标（录入错误）**：额外记录 `P299` 与 `P037` 同占 `(7,1)`；B3 的
  `(9,6)` 是真实空格。修正方法是把 `P299` 改到 `(9,6)`。
- **缺区**：`P021` 缺产量、`P045` 缺株高（CSV 空单元格）。缺区保留为显式
  缺失，**不按零产量处理**，不参与任何模型拟合。
- **单区组处理**：`T4` 的两个小区 `P001/P006` 只在 B1，因此凡涉及 `T4`
  的对比一律判为**不可估**，不使用空间平滑伪造重复。
- 产量按“总体水平 + 处理效应 + 区组效应 + 行趋势 + 列趋势 + 确定性噪声”构造，
  不含随机数，保证任何环境下结果一致。

## 阻断规则（不按最后导入覆盖）

设计审查在每次发布前执行，任一不满足都阻断分析并写入运行记录：

1. 地块编号重复（同一试验单元两条记录）；
2. `(行,列)` 坐标被多于一条记录占用；
3. 同一地块编号出现两个不同品系/处理。

冲突单元格在 SVG 中以**红色粗框**标出。修正坐标后设计检查转绿，才允许发布
新分析分支。被阻止的发布也会以 `blocked` 分支和审计记录留痕。

对比可估性按**区组覆盖**判定：处理 A、B 必须都在 ≥2 个区组中有有效观测。
仅出现在单一区组的处理对比显示“不可估”，不输出伪造的校正差或标准误。

## 空间校正候选（全部确定性、可重放）

分支发布时冻结当前全部地块快照（`branches.snapshot`），之后任何修正都不影响
已发布分支；对同一份快照重新拟合与发布结果逐值相等（页面“重放并核对”或
`POST /api/branches/{id}/replay` 返回 `match=true`）。拟合中不使用随机数，
迭代上限 200、收敛阈值 1e-8。

- **行列趋势 `rowcol`**：`y = 总均值 + 处理效应 + 行效应 + 列效应 + 残差`，
  效应每轮中心化。
- **区组随机效应 `block`**：`y = 总均值 + 处理效应 + b_block + 残差`，
  区组效应以收缩系数 λ=4 向零收缩。
- **局部相关 `local`**：在区组随机效应上增加 Papadakis 邻居协变量
  `θ · 正交邻居残差均值`（λ=3）。邻居**只取上下左右四个真实存在的相邻小区**，
  场地边界**不做环绕（torus）邻居**，缺区/排除小区不贡献邻居。

每块输出：原始值 `raw`、拟合值 `fitted`、残差 `residual`、空间校正值
`adjusted = y − 空间项`。处理均值用校正值计算，对比给出原始差、校正差、
校正前后变化、近似标准误、两侧处理实际使用的区组列表与样本量。

“排除边界地块”是分支级开关：开启后边界框上的小区整体不参与拟合（邻居查找
同样跳过），不改变底层记录。

## 页面与 HTTP 接口

- `GET /` 操作页；`GET /api/grid.svg?layer=line|raw|residual|adjusted&trait=yield|height`
- `GET /api/state` 地块、设计审查、修订、分支、运行记录
- `POST /api/plots/{id}/coordinates` `{"row":9,"col":6}` 修正行列
- `POST /api/plots/{id}/exclude` `{"excluded":true}` 排除/恢复单块
- `POST /api/branches` `{"name":"","model":"rowcol|block|local","trait":"yield|height","exclude_edge":false}`
- `GET /api/branches/{id}`、`POST /api/branches/{id}/replay`
- `GET /api/branches/{id}/grid.svg`、`GET /api/branches/{id}/export.csv`
- `GET /api/export/runs.csv` 导出运行记录（导入/修正/排除/阻断/发布/重放/重置）
- `POST /api/reimport`、`POST /api/reset`、`GET /api/fixture`

## 复核路径

1. 首次打开：设计卡显示重复坐标 `(7,1)`，发布会被阻止并留痕；
2. 将 `P299` 修正到 `(9,6)` 后分别发布三种模型；
3. 对比表中 `T1/T2/T3` 两两可估（标注使用区组与 n），涉及 `T4` 全部不可估；
4. 网格切换到残差/校正值图层，缺区显示黄色斜线、边界地块带蓝点；
5. 点“重放并核对”得到一致结果，导出分支 CSV 与运行记录 CSV；
6. “清空并复核”后数据库回到初始含冲突状态，可重复以上流程。

## 代码结构

```
cmd/server              HTTP 服务入口
internal/domain         地块模型、设计审查、三种空间模型与对比计算
internal/fixtures       嵌入的固定 CSV fixture 与解析
internal/store          SQLite：地块/修订/分支快照/运行记录
internal/httpapi        HTTP 路由、服务编排、SVG 渲染、操作页(static/index.html)
```
