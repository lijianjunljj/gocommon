# IP地理位置查询 - QQWry.dat 数据库

## 数据库文件下载

### 方式一：纯真网络官方（推荐）

**官方网站**：http://www.cz88.net/

**下载地址**：
- 直接下载：http://www.cz88.net/ip/
- 或者访问：http://update.cz88.net/ip/qqwry.rar

**注意**：纯真网络计划从2024年10月起停止维护QQWry.dat格式，建议关注官方公告。

### 方式二：GitHub开源项目

以下GitHub项目提供自动更新的QQWry.dat文件：

1. **metowolf/qqwry.dat**
   - GitHub: https://github.com/metowolf/qqwry.dat
   - 提供自动更新的数据库文件

2. **lionsoul2014/ip2region**
   - GitHub: https://github.com/lionsoul2014/ip2region
   - 提供多种格式的IP数据库

### 方式三：其他下载源

1. **IPIP.net**
   - 网站：https://www.ipip.net/
   - 提供免费和付费的IP数据库

2. **17mon/ipdb**
   - GitHub: https://github.com/17mon/ipdb
   - 提供IP数据库文件

## 使用方法

### 1. 下载数据库文件

下载 `QQWry.dat` 文件后，将其放置在项目目录下。

### 2. 初始化数据库

```go
package main

import (
    "fmt"
    "github.com/lijianjunljj/gocommon/utils/location"
)

func main() {
    // 方式1：使用默认路径（当前目录下的 qqwry.dat）
    err := location.Init("")
    if err != nil {
        fmt.Printf("初始化失败: %v\n", err)
        return
    }
    defer location.Close()

    // 方式2：指定数据库文件路径
    // err := location.Init("/path/to/qqwry.dat")
    
    // 查询IP地理位置
    info, err := location.GetLocationByIP("114.114.114.114")
    if err != nil {
        fmt.Printf("查询失败: %v\n", err)
        return
    }

    fmt.Printf("国家: %s\n", info.Country)
    fmt.Printf("省份: %s\n", info.Province)
    fmt.Printf("城市: %s\n", info.City)
    fmt.Printf("区县: %s\n", info.District)
}
```

## 数据库更新

建议定期更新QQWry.dat文件以获取最新的IP地址信息：

1. **手动更新**：从上述下载源下载最新版本，替换旧文件
2. **自动更新**：可以编写脚本定期从GitHub等源下载最新版本

## 注意事项

1. **文件格式**：确保下载的是QQWry.dat格式，不是其他格式
2. **文件编码**：QQWry.dat使用GBK编码，代码会自动处理编码转换
3. **文件大小**：通常QQWry.dat文件大小在几MB到几十MB之间
4. **更新频率**：建议每月更新一次数据库文件

## 替代方案

如果QQWry.dat格式停止维护，可以考虑：

1. **CZDB格式**：纯真网络的新格式
2. **IPIP.net数据库**：提供多种格式
3. **ip2region**：开源的IP定位库，支持多种数据库格式

## 常见问题

**Q: 数据库文件应该放在哪里？**  
A: 可以放在项目根目录，或任何可访问的路径，通过 `Init()` 函数指定路径即可。

**Q: 如何验证数据库文件是否正确？**  
A: 尝试查询一个已知的IP地址，如 `114.114.114.114`（应该返回江苏省南京市）。

**Q: 数据库文件损坏怎么办？**  
A: 重新下载数据库文件并替换即可。

**Q: 查询结果出现乱码怎么办？**  
A: QQWry.dat文件使用GBK编码，需要正确的编码转换。解决方法：

1. **更新vendor目录**（推荐）：
   ```bash
   go mod vendor
   ```
   这会下载 `golang.org/x/text/encoding/simplifiedchinese` 包到vendor目录。

2. **如果仍有问题**，检查vendor目录中是否存在：
   ```
   vendor/golang.org/x/text/encoding/simplifiedchinese/
   ```
   如果不存在，手动运行 `go mod vendor` 命令。

3. **验证编码转换**：
   确保代码中正确使用了GBK到UTF-8的转换函数。
