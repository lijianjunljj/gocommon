# 修复GBK编码乱码问题

## 问题描述

从 [metowolf/qqwry.dat](https://github.com/metowolf/qqwry.dat) 下载的QQWry.dat文件使用GBK编码，如果直接读取会出现乱码。

## 解决方法

### 方法1：更新vendor目录（推荐）

在项目根目录运行：

```bash
go mod vendor
```

这会下载 `golang.org/x/text/encoding/simplifiedchinese` 包到vendor目录。

### 方法2：修改gbkToUtf8函数

找到 `vendor/github.com/lijianjunljj/gocommon/utils/location/location.go` 文件中的 `gbkToUtf8` 函数，替换为：

```go
import (
    "golang.org/x/text/encoding/simplifiedchinese"
    "golang.org/x/text/transform"
)

// gbkToUtf8 GBK编码转UTF-8编码
func gbkToUtf8(gbk []byte) (string, error) {
    decoder := simplifiedchinese.GBK.NewDecoder()
    utf8Bytes, _, err := transform.Bytes(decoder, gbk)
    if err != nil {
        return "", fmt.Errorf("GBK转UTF-8失败: %w", err)
    }
    return string(utf8Bytes), nil
}
```

### 方法3：使用第三方库

如果无法使用golang.org/x/text，可以使用其他GBK转换库，如：
- github.com/axgle/mahonia

## 验证修复

修复后，测试查询IP地址：

```go
info, err := location.GetLocationByIP("114.114.114.114")
if err != nil {
    log.Fatal(err)
}
fmt.Printf("省份: %s, 城市: %s\n", info.Province, info.City)
// 应该输出: 省份: 江苏省, 城市: 南京市
```

如果输出正确的中文，说明修复成功。
