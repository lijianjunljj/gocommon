package main

import (
	"fmt"
	"github.com/lijianjunljj/gocommon/utils/decimal"
)

func main() {
	v := decimal.Mul(0, 1.56, 4.56, 7.89, 1.02)

	fmt.Println("v:", v)
}
