# Inco — Copilot 使用手册

## 什么是 Inco

Inco 是 Go 的编译期断言引擎。在注释中写 `// @inco:` 指令，inco 自动生成对应的 `if` 守卫块，通过 `go build -overlay` 注入编译，源文件零侵入。

## 核心规则

### 1. `@inco:` 是守卫，不是逻辑

**用 `@inco:`**：nil 检查、错误检查、范围验证、前置条件  
**用 `if`**：业务分支、条件选择、流程控制

```go
// ✅ 守卫 → @inco:
// @inco: db != nil
// @inco: err == nil, -panic(err)
// @inco: len(s) > 0, -return(0, fmt.Errorf("empty"))

// ✅ 逻辑 → if
if val < lo { return lo }
if cmd == "build" { runBuild() }
```

### 2. 两种指令形式

**Standalone**（整行是注释）：
```go
// @inco: x != nil
// @inco: x > 0, -panic("must be positive")
```

**Inline**（代码行尾部追加指令）：
```go
_ = err // @inco: err == nil, -panic(err)
_ = skip // @inco: !skip, -return(filepath.SkipDir)
```

Inline 形式用于变量只在指令中使用的场景——`_ = var` 消除编译器 unused variable 报错。

### 3. 可用动作

| 动作 | 语法 | 含义 |
|------|------|------|
| panic（默认） | `// @inco: <expr>` | 自动生成 panic 消息 |
| panic（自定义） | `// @inco: <expr>, -panic("msg")` | 自定义 panic |
| return | `// @inco: <expr>, -return(vals...)` | 返回指定值 |
| return（裸） | `// @inco: <expr>, -return` | 裸 return |
| continue | `// @inco: <expr>, -continue` | continue 循环 |
| break | `// @inco: <expr>, -break` | break 循环 |

### 4. 指令语义

`// @inco: <expr>` 的含义是"expr 必须为 true"。生成代码为 `if !(<expr>) { action }`。

注意表达式是**正向**的——写你期望成立的条件：
```go
// @inco: err == nil, -panic(err)    // 期望无错误
// @inco: n > 0, -continue           // 期望 n 为正
// @inco: !skip, -return(filepath.SkipDir)  // 期望不跳过
```

## 文件约定

- `foo.inco.go` — 包含 `@inco:` 指令的源文件（推荐命名）
- `.inco_cache/` — 生成的影子文件和 overlay.json（加入 .gitignore）
- `foo_test.go` — 测试文件（不会被 inco 处理）

## 编写规范

### 写新代码时

1. 防御性检查用 `// @inco:`，不用 `if`
2. 错误处理优先用 inline 形式：`_ = err // @inco: err == nil, -panic(err)`
3. 函数入口的参数校验用 standalone：`// @inco: root != ""`
4. 循环中的过滤条件可以用 `-continue` 或 `-break`

### if → @inco: 转换模板

```go
// 转换前：
if err != nil { return nil, err }
// 转换后：
_ = err // @inco: err == nil, -return(nil, err)

// 转换前：
if x == nil { panic("x is nil") }
// 转换后：
// @inco: x != nil, -panic("x is nil")

// 转换前：
if !valid { continue }
// 转换后：
_ = valid // @inco: valid, -continue

// 转换前：
if n == target { break }
// 转换后：
_ = n // @inco: n != target, -break
```

### 不要转换的 if

- 业务逻辑分支：`if val < lo { return lo }`
- 有 else 的条件：`if x { A } else { B }`
- 功能性判断：`if cmd == "build" { ... }`
- 含副作用的条件块（多行 body）

## 常用命令

```bash
# 安装
go install github.com/imnive-design/inco-go/cmd/inco@latest

# 日常开发
inco build ./...     # gen + build
inco test ./...      # gen + test
inco audit .         # 覆盖率报告

# 发布（无需 inco 即可 go build）
inco release .       # .inco.go → .inco（备份）+ .go（含守卫）
inco release clean . # 恢复

# 清理
inco clean .         # 删除 .inco_cache/
```

## 审计指标

`inco audit` 报告两个关键指标：

- **inco/(if+inco)**：守卫指令占所有条件检查的比例，目标 > 50%
- **函数覆盖率**：有至少一个 `@inco:` 的函数比例，越高越好

剩余的 `if` 应该都是真正的业务逻辑。
